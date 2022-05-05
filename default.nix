# vim: set tabstop=2 shiftwidth=2 expandtab:
{pkgs ? import <nixpkgs> {}}:

let
  package-name = "fdroid-news";
in pkgs.buildGoModule rec{
  name = "${package-name}";
  pname = "${package-name}";
  src = ./.;
  vendorSha256 = "sha256-ZuKhM+grt1oATUf0MAYu95ZM1aqwlykdwxeEs5PrRIQ=";
}
