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

    daemonUrl = mkOption {
      type = types.str;
      description = "URL where the sophon daemon is reachable. Set automatically to localhost when daemon.enable is true.";
      example = "https://foxtrotbase.swallow-galaxy.ts.net";
    };

    nodeName = mkOption {
      type = types.str;
      description = "Node name identifying this machine to the daemon.";
    };

    hookCommand = mkOption {
      type = types.str;
      readOnly = true;
      default = concatStringsSep " " [
        "${cfg.package}/bin/sophon"
        "hook"
        "--daemon-url ${cfg.daemonUrl}"
        "--ntfy-url ${cfg.ntfyUrl}"
        "--node-name ${cfg.nodeName}"
      ];
      defaultText = literalExpression ''"''${cfg.package}/bin/sophon hook --daemon-url ''${cfg.daemonUrl} --ntfy-url ''${cfg.ntfyUrl} --node-name ''${cfg.nodeName}"'';
      description = "Full hook command with daemon URL, ntfy URL, and node name baked in. Use this in Claude Code hook configuration.";
    };

    minSessionAge = mkOption {
      type = types.int;
      default = 120;
      description = "Minimum session age in seconds before Stop sends a completion notification.";
    };

    daemon = {
      enable = mkOption {
        type = types.bool;
        default = false;
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

    agent = {
      enable = mkOption {
        type = types.bool;
        default = false;
        description = "Run the sophon agent as a systemd user service.";
      };

      port = mkOption {
        type = types.int;
        default = 2588;
        description = "Port for the sophon agent to listen on.";
      };

      autoStart = mkOption {
        type = types.bool;
        default = true;
        description = "Start the agent automatically on user login.";
      };

      logLevel = mkOption {
        type = types.enum [ "debug" "info" "warn" "error" ];
        default = "info";
        description = "Log level for the agent.";
      };
    };
  };

  config = mkIf cfg.enable (mkMerge [
    {
      home.packages = [ cfg.package ];
    }

    # When daemon is enabled locally, default daemonUrl to localhost
    (mkIf cfg.daemon.enable {
      services.sophon.daemonUrl = mkDefault "http://127.0.0.1:${toString cfg.daemon.port}";

      # Systemd user service for the daemon
      systemd.user.services.sophon = {
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

        Environment = [
          "SOPHON_DAEMON_URL=${cfg.daemonUrl}"
          "SOPHON_NTFY_URL=${cfg.ntfyUrl}"
        ];
      };

      Install = mkIf cfg.daemon.autoStart {
        WantedBy = [ "default.target" ];
      };
    };
    })

    # When agent is enabled, run the per-node agent service
    (mkIf cfg.agent.enable {
      systemd.user.services.sophon-agent = {
      Unit = {
        Description = "Sophon Agent - per-node transcript and tmux proxy";
        Documentation = "https://github.com/phinze/sophon";
        After = [ "network.target" ];
      };

      Service = {
        Type = "simple";
        ExecStart = concatStringsSep " " [
          "${cfg.package}/bin/sophon"
          "agent"
          "--port ${toString cfg.agent.port}"
          "--daemon-url ${cfg.daemonUrl}"
          "--node-name ${cfg.nodeName}"
          "--log-level ${cfg.agent.logLevel}"
        ];
        Restart = "on-failure";
        RestartSec = "5s";

        # Agent needs tmux access for send-keys and pane focus detection
        Environment = [
          "PATH=${pkgs.tmux}/bin:/run/current-system/sw/bin"
          "TMUX_TMPDIR=%t"
        ];
      };

      Install = mkIf cfg.agent.autoStart {
        WantedBy = [ "default.target" ];
      };
    };
    })
  ]);
}
