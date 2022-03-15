# vim: set tabstop=2 shiftwidth=2 expandtab:
{pkgs ? import <nixpkgs> {}}:
with pkgs;

buildGoModule {
  name = "fdroid-news";
  pname = self.name;
  src = ./.;
  vendorSha256 = "sha256-FlBUN8B1U+ELpia5R/5oNflD/IMWIQ+mXhdP2iaWkjk=";
}
