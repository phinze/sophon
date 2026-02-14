self:
{
  config,
  lib,
  pkgs,
  ...
}:

with lib;
let
  cfg = config.services.sophon;
in
{
  options.services.sophon = {
    enable = mkEnableOption "sophon notification + response relay for Claude Code";

    package = mkOption {
      type = types.package;
      default = self.packages.${pkgs.system}.default;
      defaultText = literalExpression "sophon.packages.\${system}.default";
      description = "The sophon package to use.";
    };

    ntfyUrl = mkOption {
      type = types.str;
      description = "ntfy server URL for push notifications.";
      example = "https://foxtrotbase.swallow-galaxy.ts.net/claude";
    };

    baseUrl = mkOption {
      type = types.str;
      description = "Public base URL where sophon web UI is accessible.";
      example = "https://foxtrotbase.swallow-galaxy.ts.net";
    };

    minSessionAge = mkOption {
      type = types.int;
      default = 120;
      description = "Minimum session age in seconds before Stop sends a completion notification.";
    };

    daemon = {
      enable = mkOption {
        type = types.bool;
        default = true;
        description = "Run the sophon daemon as a systemd user service.";
      };

      port = mkOption {
        type = types.int;
        default = 2587;
        description = "Port for the sophon daemon to listen on.";
      };

      autoStart = mkOption {
        type = types.bool;
        default = true;
        description = "Start the daemon automatically on user login.";
      };

      logLevel = mkOption {
        type = types.enum [ "debug" "info" "warn" "error" ];
        default = "info";
        description = "Log level for the daemon.";
      };
    };
  };

  config = mkIf cfg.enable {
    home.packages = [ cfg.package ];

    # Systemd user service for the daemon
    systemd.user.services.sophon = mkIf cfg.daemon.enable {
      Unit = {
        Description = "Sophon - Claude Code notification + response relay";
        Documentation = "https://github.com/phinze/sophon";
        After = [ "network.target" ];
      };

      Service = {
        Type = "simple";
        ExecStart = concatStringsSep " " [
          "${cfg.package}/bin/sophon"
          "daemon"
          "--port ${toString cfg.daemon.port}"
          "--ntfy-url ${cfg.ntfyUrl}"
          "--base-url ${cfg.baseUrl}"
          "--min-session-age ${toString cfg.minSessionAge}"
          "--log-level ${cfg.daemon.logLevel}"
        ];
        Restart = "on-failure";
        RestartSec = "5s";

        # Security hardening
        PrivateTmp = true;
        ProtectSystem = "strict";
        ProtectHome = "read-only";

        # tmux must be in PATH for send-keys responses
        Environment = [
          "SOPHON_DAEMON_URL=http://127.0.0.1:${toString cfg.daemon.port}"
          "SOPHON_NTFY_URL=${cfg.ntfyUrl}"
          "PATH=${pkgs.tmux}/bin:/run/current-system/sw/bin"
        ];
      };

      Install = mkIf cfg.daemon.autoStart {
        WantedBy = [ "default.target" ];
      };
    };
  };
}
