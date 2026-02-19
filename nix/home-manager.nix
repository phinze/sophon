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

    baseUrl = mkOption {
      type = types.str;
      default = "";
      description = "Public base URL where sophon web UI is accessible. Optional when using relative URLs.";
      example = "https://sophon.example.com";
    };

    daemonUrl = mkOption {
      type = types.str;
      description = "URL where the sophon daemon is reachable (e.g. Miren-deployed instance).";
      example = "https://sophon.example.com";
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
        "--node-name ${cfg.nodeName}"
      ];
      defaultText = literalExpression ''"''${cfg.package}/bin/sophon hook --daemon-url ''${cfg.daemonUrl} --node-name ''${cfg.nodeName}"'';
      description = "Full hook command with daemon URL and node name baked in. Use this in Claude Code hook configuration.";
    };

    minSessionAge = mkOption {
      type = types.int;
      default = 120;
      description = "Minimum seconds since last activity before a turn-end sends a completion notification.";
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

      advertiseUrl = mkOption {
        type = types.str;
        default = "";
        description = "URL the daemon should use to reach this agent. Also determines the listen address (binds to the URL's hostname). When empty, defaults to http://127.0.0.1:<port>.";
        example = "http://phinze-mrn-mbp.swallow-galaxy.ts.net:2588";
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

    # When agent is enabled on Linux, run as systemd user service
    (mkIf (cfg.agent.enable && pkgs.stdenv.isLinux) {
      systemd.user.services.sophon-agent = {
      Unit = {
        Description = "Sophon Agent - per-node transcript and tmux proxy";
        Documentation = "https://github.com/phinze/sophon";
        After = [ "network.target" ];
      };

      Service = {
        Type = "simple";
        ExecStart = concatStringsSep " " ([
          "${cfg.package}/bin/sophon"
          "agent"
          "--port ${toString cfg.agent.port}"
          "--daemon-url ${cfg.daemonUrl}"
          "--node-name ${cfg.nodeName}"
          "--log-level ${cfg.agent.logLevel}"
        ] ++ optional (cfg.agent.advertiseUrl != "") "--advertise-url ${cfg.agent.advertiseUrl}");
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

    # When agent is enabled on macOS, run as launchd agent
    (mkIf (cfg.agent.enable && pkgs.stdenv.isDarwin) {
      launchd.agents.sophon-agent = {
        enable = cfg.agent.autoStart;
        config = {
          Label = "ph.inze.sophon-agent";
          ProgramArguments = [
            "${cfg.package}/bin/sophon"
            "agent"
            "--port" (toString cfg.agent.port)
            "--daemon-url" cfg.daemonUrl
            "--node-name" cfg.nodeName
            "--log-level" cfg.agent.logLevel
          ] ++ optionals (cfg.agent.advertiseUrl != "") [ "--advertise-url" cfg.agent.advertiseUrl ];
          KeepAlive = true;
          RunAtLoad = true;
          StandardOutPath = "/tmp/sophon-agent.log";
          StandardErrorPath = "/tmp/sophon-agent.log";
          EnvironmentVariables = {
            PATH = "${pkgs.tmux}/bin:/usr/bin:/bin";
          };
        };
      };
    })
  ]);
}
