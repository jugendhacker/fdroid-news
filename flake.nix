# vim: set tabstop=2 shiftwidth=2 expandtab:
{
  description = "fdroid-news XMPP bot";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/release-21.11";
  };

  outputs = inputs@{ self, nixpkgs }: {
    packages.x86_64-linux.fdroid-news = import ./default.nix {pkgs = inputs.nixpkgs.legacyPackages.x86_64-linux;};

    defaultPackage.x86_64-linux = self.packages.x86_64-linux.fdroid-news;

    nixosModule = import ./module.nix {};
  };
}
