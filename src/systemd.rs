use std::process::Command;

const SYSTEMD_SERVICE_NAME: &str = "whoisthat-core.service";

pub(crate) fn systemd_service_path() -> std::path::PathBuf {
    crate::config::config_dir()
        .parent()
        .map(|p| p.to_path_buf())
        .unwrap_or_else(|| std::path::PathBuf::from("/tmp"))
        .join("systemd")
        .join("user")
        .join(SYSTEMD_SERVICE_NAME)
}

pub(crate) fn systemd_is_enabled() -> bool {
    if !crate::platform::has_systemd() {
        return false;
    }
    Command::new("systemctl")
        .arg("--user")
        .arg("is-enabled")
        .arg(SYSTEMD_SERVICE_NAME)
        .output()
        .map(|o| o.status.success())
        .unwrap_or(false)
}

pub(crate) fn linger_is_enabled() -> bool {
    if !crate::platform::has_systemd() {
        return false;
    }
    let user = std::env::var("USER").unwrap_or_default();
    if user.is_empty() {
        return false;
    }
    Command::new("loginctl")
        .arg("show-user")
        .arg(&user)
        .arg("--property=Linger")
        .output()
        .map(|o| {
            let s = String::from_utf8_lossy(&o.stdout);
            s.contains("Linger=yes")
        })
        .unwrap_or(false)
}

/// Escape a value for a systemd unit: `%` starts a specifier and must be
/// doubled, and `ExecStart`/`Environment` values are quoted.
fn unit_escape(value: &str) -> String {
    value
        .replace('%', "%%")
        .replace('\\', "\\\\")
        .replace('"', "\\\"")
}

fn generate_unit_file(core_path: &str) -> String {
    let core = unit_escape(core_path);
    format!(
        "[Unit]\n\
         Description=WhoisThat VPN Core\n\
         After=network-online.target\n\
         Wants=network-online.target\n\
         \n\
         [Service]\n\
         Type=simple\n\
         ExecStart=\"{core}\"\n\
         Environment=\"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\"\n\
         Restart=on-failure\n\
         RestartSec=5\n\
         \n\
         [Install]\n\
         WantedBy=default.target\n"
    )
}

/// System unit (K-14) that installs the armed kill-switch before the network
/// comes up, from the config on disk, so a reboot never opens a leak window
/// before the user session starts the core. It arms with an empty server
/// allowlist; the core re-arms with the real IPs when it connects.
///
/// Modelled on nftables.service: oneshot + RemainAfterExit, ordered before
/// network-pre.target, and deliberately no ExecStop: stopping the unit (or
/// shutdown) must not drop the ruleset.
pub(crate) fn generate_boot_unit_file(core_path: &str, config_dir: &std::path::Path) -> String {
    let core = unit_escape(core_path);
    let dir = unit_escape(&config_dir.to_string_lossy());
    format!(
        "[Unit]\n\
         Description=WhoisThat kill-switch (arm before network)\n\
         Documentation=https://github.com/kvunoff/whoisthat\n\
         DefaultDependencies=no\n\
         Wants=network-pre.target\n\
         Before=network-pre.target\n\
         After=local-fs.target\n\
         RequiresMountsFor={dir_raw}\n\
         Conflicts=shutdown.target\n\
         Before=shutdown.target\n\
         \n\
         [Service]\n\
         Type=oneshot\n\
         RemainAfterExit=yes\n\
         Environment=\"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\"\n\
         ExecStart=\"{core}\" --arm-boot \"{dir}\"\n\
         \n\
         [Install]\n\
         WantedBy=sysinit.target\n",
        dir_raw = config_dir
            .to_string_lossy()
            .replace('%', "%%")
            .replace(' ', "\\x20"),
    )
}

fn enable_linger_via_pkexec() -> bool {
    let user = std::env::var("USER").unwrap_or_default();
    if user.is_empty() {
        return false;
    }
    Command::new("pkexec")
        .arg("loginctl")
        .arg("enable-linger")
        .arg(&user)
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .status()
        .map(|s| s.success())
        .unwrap_or(false)
}

pub(crate) fn setup_systemd_service(core_path: &str) -> Result<(), String> {
    let core =
        std::path::absolute(core_path).map_err(|e| format!("cannot resolve core path: {e}"))?;
    let core_str = core.to_string_lossy().to_string();

    let unit = generate_unit_file(&core_str);
    let service_path = systemd_service_path();
    if let Some(parent) = service_path.parent() {
        std::fs::create_dir_all(parent).map_err(|e| format!("cannot create systemd dir: {e}"))?;
    }
    std::fs::write(&service_path, &unit).map_err(|e| format!("cannot write service file: {e}"))?;

    let status = Command::new("systemctl")
        .arg("--user")
        .arg("daemon-reload")
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .status()
        .map_err(|e| format!("daemon-reload failed: {e}"))?;
    if !status.success() {
        return Err("systemctl --user daemon-reload failed".into());
    }

    let status = Command::new("systemctl")
        .arg("--user")
        .arg("enable")
        .arg(SYSTEMD_SERVICE_NAME)
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .status()
        .map_err(|e| format!("enable failed: {e}"))?;
    if !status.success() {
        return Err("systemctl --user enable failed".into());
    }

    if !linger_is_enabled() && !enable_linger_via_pkexec() {
        return Err(
            "Could not enable lingering. Run manually: sudo loginctl enable-linger $USER".into(),
        );
    }

    Ok(())
}

pub(crate) fn teardown_systemd_service() -> Result<(), String> {
    let _ = Command::new("systemctl")
        .arg("--user")
        .arg("disable")
        .arg(SYSTEMD_SERVICE_NAME)
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .status();

    let service_path = systemd_service_path();
    let _ = std::fs::remove_file(&service_path);

    let status = Command::new("systemctl")
        .arg("--user")
        .arg("daemon-reload")
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .status()
        .map_err(|e| format!("daemon-reload failed: {e}"))?;
    if !status.success() {
        return Err("systemctl --user daemon-reload failed".into());
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    const BOOT_SERVICE_NAME: &str = "whoisthat-killswitch-boot.service";

    #[test]
    fn unit_file_escapes_paths() {
        let unit = generate_unit_file("/tmp/with space/100%/whoisthat-core");

        assert!(unit.contains("ExecStart=\"/tmp/with space/100%%/whoisthat-core\""));
        // The log level is applied over IPC, so the unit must not pin a stale
        // env value.
        assert!(!unit.contains("WHOISTHAT_LOG_LEVEL"));
        // A stray space after the executable breaks systemd's argument parsing.
        assert!(!unit.contains("\" \n"));
    }

    // F-03: the user manager's PATH includes user-writable dirs; the core must
    // not resolve nft/ip/xray through it.
    #[test]
    fn unit_file_pins_path_to_system_directories() {
        let unit = generate_unit_file("/usr/bin/whoisthat-core");
        assert!(unit.contains(
            "Environment=\"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\"\n"
        ));
    }

    // K-14: the boot unit must order itself before the network comes up and
    // arm from the on-disk config, with no dependency on a logged-in session.
    #[test]
    fn boot_unit_arms_before_network_pre_from_disk_config() {
        let unit = generate_boot_unit_file(
            "/usr/bin/whoisthat-core",
            std::path::Path::new("/home/a03/.config/whoisthat"),
        );
        for want in [
            "DefaultDependencies=no\n",
            "Before=network-pre.target\n",
            "Wants=network-pre.target\n",
            "RequiresMountsFor=/home/a03/.config/whoisthat\n",
            "Type=oneshot\n",
            "RemainAfterExit=yes\n",
            "ExecStart=\"/usr/bin/whoisthat-core\" --arm-boot \"/home/a03/.config/whoisthat\"\n",
            "WantedBy=sysinit.target\n",
            "Environment=\"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\"\n",
        ] {
            assert!(unit.contains(want), "boot unit missing {want:?}:\n{unit}");
        }
        // Stopping the unit (shutdown, `systemctl stop`) must never drop the
        // ruleset: no ExecStop, and no user-session dependencies.
        assert!(!unit.contains("ExecStop"));
        assert!(!unit.contains("default.target"));
        assert!(!unit.contains("network-online"));
    }

    #[test]
    fn boot_unit_escapes_paths() {
        let unit = generate_boot_unit_file(
            "/opt/100%/whoisthat-core",
            std::path::Path::new("/home/we ird/100%/whoisthat"),
        );
        assert!(unit.contains(
            "ExecStart=\"/opt/100%%/whoisthat-core\" --arm-boot \"/home/we ird/100%%/whoisthat\"\n"
        ));
    }

    #[test]
    fn boot_unit_passes_systemd_analyze_verify() {
        if Command::new("systemd-analyze")
            .arg("--version")
            .output()
            .is_err()
        {
            return;
        }
        let dir = std::env::temp_dir().join(format!("wit-boot-unit-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join(BOOT_SERVICE_NAME);
        // /bin/true exists everywhere, so verify checks syntax and ordering,
        // not the install path.
        std::fs::write(
            &path,
            generate_boot_unit_file("/bin/true", std::path::Path::new("/tmp/whoisthat")),
        )
        .unwrap();
        let out = Command::new("systemd-analyze")
            .arg("verify")
            .arg(&path)
            .output()
            .unwrap();
        let stderr = String::from_utf8_lossy(&out.stderr);
        let _ = std::fs::remove_dir_all(&dir);
        // Only complaints about our own unit count; the sandbox may lack
        // unrelated system units.
        let ours: Vec<_> = stderr
            .lines()
            .filter(|l| l.contains(BOOT_SERVICE_NAME))
            .collect();
        assert!(ours.is_empty(), "systemd-analyze verify: {ours:?}");
    }
}
