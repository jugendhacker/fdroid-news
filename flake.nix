# vim: set tabstop=2 shiftwidth=2 expandtab:
{
  description = "fdroid-news XMPP bot";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/release-22.05";
  };

  outputs = inputs@{ self, nixpkgs }:
  let
    pkgs = import nixpkgs {
      system = "x86_64-linux";
      overlays = [ self.overlays.default ];
    };
  in
  rec {
    overlays.default = final: prev: {
      fdroid-news = with final; let
        package-name = "fdroid-news";
      in pkgs.buildGoModule rec{
        name = "${package-name}";
        pname = "${package-name}";
        src = ./.;
        vendorSha256 = "sha256-AQS9Q+5u2lVW20nQLQZljaZeadvuRSfSKDBftfucKt8=";
      };
    };

    packages.x86_64-linux.fdroid-news = pkgs.fdroid-news;
    nixosModules.fdroid-news = {
      imports = [ ./module.nix ];
      nixpkgs.overlays = [ self.overlays.default ];
    };
  };
}
