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
      fdroid-news = let
        package-name = "fdroid-news";
      in final.pkgs.buildGo118Module rec{
        name = "${package-name}";
        pname = "${package-name}";
        src = ./.;
        proxyVendor = true;
        vendorSha256 = "sha256-68tYbpY5Y0V6rZ2mXnjmSuWcq7/YsI9Pb8bpJ6nzK9E=";
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
