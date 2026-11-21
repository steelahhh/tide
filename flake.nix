{
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "aarch64-darwin" "x86_64-darwin" "x86_64-linux" "aarch64-linux" ];
      forAll = f: nixpkgs.lib.genAttrs systems (s: f nixpkgs.legacyPackages.${s});
    in
    {
      packages = forAll (pkgs: {
        default = pkgs.buildGoModule {
          pname = "tide";
          version = "0.1.0";
          src = ./.;
          vendorHash = null; # no dependencies
          nativeBuildInputs = [ pkgs.makeWrapper ];
          # GNU rsync first on PATH; macOS's openrsync lacks needed flags.
          postInstall = ''
            wrapProgram $out/bin/tide --prefix PATH : ${pkgs.lib.makeBinPath [ pkgs.rsync ]}
          '';
        };
      });

      devShells = forAll (pkgs: {
        default = pkgs.mkShell { packages = [ pkgs.go pkgs.rsync ]; };
      });
    };
}