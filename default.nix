# vim: set tabstop=2 shiftwidth=2 expandtab:
{pkgs ? import <nixpkgs> {}}:

let
  package-name = "fdroid-news";
in pkgs.buildGoModule rec{
  name = "${package-name}";
  pname = "${package-name}";
  src = ./.;
  vendorSha256 = "sha256-AQS9Q+5u2lVW20nQLQZljaZeadvuRSfSKDBftfucKt8=";
}
