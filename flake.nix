{
  description = "The lazier way to manage everything docker";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.05";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    let
      supportedSystems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];

      gitCommit = self.rev or self.dirtyRev or "dev";
      version = "0.24.1";

    in
    flake-utils.lib.eachSystem supportedSystems (
      system:
      let
        pkgs = import nixpkgs { inherit system; };

        lazydocker = pkgs.buildGoModule rec {
          pname = "lazydocker";
          inherit version;

          src = ./.;

          vendorHash = null;

          # Disable integration tests that require specific environment
          doCheck = false;

          nativeBuildInputs = with pkgs; [ git ];

          buildInputs = with pkgs; [ git ];

          ldflags = [
            "-s"
            "-w"
            "-X main.commit=${gitCommit}"
            "-X main.version=${version}"
            "-X main.buildSource=nix"
          ];

          meta = with pkgs.lib; {
            description = "The lazier way to manage everything docker";
            homepage = "https://github.com/jesseduffield/lazydocker";
            license = licenses.mit;
            maintainers = [ "jesseduffield" ];
            platforms = platforms.unix;
            mainProgram = "lazydocker";
          };
        };

      in
      {
        packages = {
          default = lazydocker;
          inherit lazydocker;
        };

        apps = {
          default = flake-utils.lib.mkApp {
            drv = lazydocker;
            name = "lazydocker";
          };
          lazydocker = flake-utils.lib.mkApp {
            drv = lazydocker;
            name = "lazydocker";
          };
        };

        devShells.default = pkgs.mkShell {
          name = "lazydocker-dev";

          buildInputs = with pkgs; [
            # Go toolchain
            go_1_24
            gotools
            golangci-lint

            # Development tools
            git
            gnumake
          ];

          shellHook = ''
            echo "Lazydocker development environment"
            echo "Go version: $(go version)"
            echo "Git version: $(git --version)"
            echo ""
          '';

          # Environment variables for development
          CGO_ENABLED = "0";
        };

        # Formatting check
        formatter = pkgs.nixpkgs-fmt;

        # Development checks
        checks = {
          # Ensure the package builds
          build = lazydocker;

          # Format check
          format =
            pkgs.runCommand "check-format"
              {
                buildInputs = [ pkgs.nixpkgs-fmt ];
              }
              ''
                nixpkgs-fmt --check ${./.}
                touch $out
              '';
        };
      }
    )
    // {
      # Global overlay for other flakes to use
      overlays.default = final: prev: {
        lazydocker = self.packages.${final.system}.lazydocker;
      };

      # CI/CD support
      hydraJobs = self.packages;
    };
}
