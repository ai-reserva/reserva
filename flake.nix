{
  description = "A terminal UI for managing Claude Code sessions";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      forAllSystems = nixpkgs.lib.genAttrs [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          inherit (pkgs) lib;
        in
        {
          default = pkgs.buildGoModule (finalAttrs: {
            pname = "reserva";
            version = self.shortRev or self.dirtyShortRev;

            src = lib.fileset.toSource {
              root = ./.;
              fileset = lib.fileset.unions [
                ./go.mod
                ./go.sum
                (lib.fileset.fileFilter (file: file.hasExt "go") ./.)
              ];
            };

            vendorHash = "sha256-YLuYZ3c4xyQbkHumwybcQrIaBfejxY9i2h0gi2sqobc=";

            env.CGO_ENABLED = 0;
            ldflags = [
              "-s"
              "-w"
            ];

            meta = {
              description = "A terminal UI for managing Claude Code sessions";
              homepage = "https://github.com/ai-reserva/reserva";
              license = lib.licenses.mit;
              mainProgram = "reserva";
              platforms = lib.platforms.linux ++ lib.platforms.darwin;
            };
          });
        }
      );

      devShells = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = pkgs.mkShell {
            inputsFrom = [ self.packages.${system}.default ];
            packages = with pkgs; [
              gopls
              gotools
              golangci-lint
              delve
            ];
          };
        }
      );

      checks = forAllSystems (system: {
        default = self.packages.${system}.default;
      });
    };
}
