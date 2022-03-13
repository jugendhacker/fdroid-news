# vim: set tabstop=2 shiftwidth=2 expandtab:
{pkgs ? import <nixpkgs> {}}:
with pkgs;

buildGoModule {
  name = "fdroid-news";
  pname = self.name;
  src = ./.;
  vendorSha256 = "sha256-IzQqntyLKAHgxuoktLaGmZM8KppNxqvltjivIUN9fL4=";
}
