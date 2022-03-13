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
  };

  config =
    let
      cfg = config.services.fdroid-news;
      fdroid-news = pkgs.callPackage ./default.nix {};
    in mkIf cfg.enable {
      environment.etc."fdroid-news/config.yml".text = pkgs.lib.generators.toYAML {} (removeAttrs cfg ["enable"]);
      systemd.services.fdroid-news = {
        description = "fdroid-news bot";
        wantedBy = [ "multi-user.target" ];
        after    = [ "network.target" ];
        serviceConfig = {
          Type = "simple";
          ExecStart = "${fdroid-news}/bin/fdroid-news -c /etc/fdroid-news/config.yml";
          StateDirectory = "fdroid-news";
          ConfigurationDirectory = "fdroid-news";
          WorkingDirectory = "/var/lib/fdroid-news";
          Restart = "always";

          DynamicUser = true;
          NoNewPriviliges = true;
          ProtectHome = true;
          ProtectSystem = "strict";
        };
      };
    };
}
