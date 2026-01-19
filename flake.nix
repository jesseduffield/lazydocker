{
  description = "The lazier way to manage everything docker";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    systems.url = "github:nix-systems/default";
    flake-parts.url = "github:hercules-ci/flake-parts";
    flake-compat.url = "https://flakehub.com/f/edolstra/flake-compat/1.tar.gz";
    treefmt-nix.url = "github:numtide/treefmt-nix";
  };

  outputs =
    inputs@{
      self,
      flake-parts,
      systems,
      ...
    }:
    flake-parts.lib.mkFlake { inherit inputs; } {
      systems = import systems;
      imports = [
        inputs.treefmt-nix.flakeModule
      ];

      perSystem =
        {
          self',
          pkgs,
          system,
          ...
        }:
        let
          lazydocker = pkgs.buildGoModule rec {
            pname = "lazydocker";
            version = "dev";

            gitCommit = inputs.self.rev or inputs.self.dirtyRev or "dev";

            src = ./.;
            vendorHash = null;

            # Disable integration tests that require specific environment
            doCheck = false;

            ldflags = [
              "-s"
              "-w"
              "-X main.commit=${gitCommit}"
              "-X main.version=${version}"
              "-X main.buildSource=nix"
            ];

            meta = {
              description = "The lazier way to manage everything docker";
              homepage = "https://github.com/jesseduffield/lazydocker";
              license = pkgs.lib.licenses.mit;
              maintainers = [ "jesseduffield" ];
              platforms = pkgs.lib.platforms.unix;
              mainProgram = "lazydocker";
            };
          };
        in
        {
          packages = {
            default = lazydocker;
            inherit lazydocker;
          };

          devShells.default = pkgs.mkShell {
            name = "lazydocker-dev";

            buildInputs = with pkgs; [
              # Go toolchain
              go
              gotools

              gnumake
            ];

            # Environment variables for development
            CGO_ENABLED = "0";
          };

          treefmt = {
            programs.nixfmt.enable = pkgs.lib.meta.availableOn pkgs.stdenv.buildPlatform pkgs.nixfmt-rfc-style.compiler;
            programs.nixfmt.package = pkgs.nixfmt-rfc-style;
            programs.gofmt.enable = true;
          };

          checks.build = lazydocker;
        };

      flake = {
        overlays.default = final: prev: {
          lazydocker = inputs.self.packages.${final.system}.lazydocker;
        };
      };
    };
}
