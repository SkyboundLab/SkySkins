{
  description = "Website";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";

    systems.url = "github:nix-systems/default";
  };

  outputs = {
    self,
    systems,
    nixpkgs,
  }: let
    eachSystem = nixpkgs.lib.genAttrs (import systems);
  in {
    devShells = eachSystem (
      system: let
        pkgs = import nixpkgs {
          inherit system;
        };
      in {
        default = pkgs.mkShell {
          packages = [pkgs.go];
        };
      }
    );
  };
}
