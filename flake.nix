# vim: set tabstop=2 shiftwidth=2 expandtab:
{
  description = "fdroid-news XMPP bot";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/release-25.05";
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
      fdroid-news = let
        package-name = "fdroid-news";
      in final.pkgs.buildGoModule rec{
        name = "${package-name}";
        pname = "${package-name}";
        src = ./.;
        proxyVendor = true;
        vendorHash = "sha256-uGKjd8P4ZJfOTKTpAjl1Et8PtR9qtnS1SOQsxyj7Fcw=";
      };
    };

    packages.x86_64-linux = {
      fdroid-news = pkgs.fdroid-news;
      default = pkgs.fdroid-news;
    };
    nixosModules.fdroid-news = {
      imports = [ ./module.nix ];
      nixpkgs.overlays = [ self.overlays.default ];
    };
  };
}
