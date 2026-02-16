{
  description = "sophon - notification + response relay for Claude Code";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        sophon = pkgs.callPackage ./nix/package.nix {
          lastModifiedDate = self.lastModifiedDate;
          buildNpmPackage = pkgs.buildNpmPackage;
        };
      in
      {
        packages = {
          default = sophon;
          sophon = sophon;
        };

        apps.default = flake-utils.lib.mkApp {
          drv = sophon;
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
            gotools
            gnumake
            nodejs
          ];
        };
      }
    )
    // {
      homeManagerModules.default = import ./nix/home-manager.nix self;
    };
}
