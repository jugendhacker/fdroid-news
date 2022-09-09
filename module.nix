# vim: tabstop=2 shiftwidth=2 expandtab:
{ lib, pkgs, config, ... }:

with lib;
{
  options.services.fdroid-news = {
    enable = mkOption {
      type = types.bool;
      default = true;
      description = "Enable fdroid-news bot";
    };
    username = mkOption {
      type = types.str;
      description = "JID of the user the bot should use";
    };
    host = mkOption {
      type = types.str;
      description = "Host the bot connect to";
    };
    password = mkOption {
      type = types.str;
      description = "XMPP Password";
      default = "";
    };
    passwordFile = mkOption {
      type = types.str;
      description = "Optional password file";
    };
    muc = mkOption {
      type = types.str;
      description = "MUC the bot should join";
    };
    nick = mkOption {
      type = types.str;
      description = "Nick the bot should use";
    };
    repos = mkOption {
      type = types.listOf types.str;
      description = "List of repos the bot should monitor";
    };
    user = mkOption {
      type = types.str;
    };
    group = mkOption {
      type = types.str;
    };
  };

  config =
    let
      cfg = config.services.fdroid-news;
      fdroid-news = pkgs.fdroid-news;
    in mkIf cfg.enable {
      environment.etc."fdroid-news/config.yml".source = (pkgs.formats.yaml {}).generate "config.yml" {xmpp = removeAttrs cfg ["enable" "repos"]; repos = cfg.repos;};
      systemd.services.fdroid-news = {
        description = "fdroid-news bot";
        wantedBy = [ "multi-user.target" ];
        after    = [ "network.target" ];
        serviceConfig = {
          Type = "simple";
          ExecStart = "${fdroid-news}/bin/fdroid-news -c /etc/fdroid-news/config.yml" + optionalString (cfg.passwordFile != null) " -p ${cfg.passwordFile}";
          StateDirectory = "fdroid-news";
          ConfigurationDirectory = "fdroid-news";
          WorkingDirectory = "/var/lib/fdroid-news";
          Restart = "always";
          RestartSec = "5min";

          User = cfg.user;
          Group = cfg.group;
          NoNewPriviliges = true;
          ProtectHome = "tmpfs";
          ProtectSystem = "strict";
        };
      };
    };
}
