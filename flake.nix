{
  description = "Arg jobs analytics";

  inputs = {
    nixpkgs.url = "nixpkgs/nixos-unstable";
  };

  outputs = { nixpkgs, ... } :
    let
      system = "x86_64-linux";
      pkgs = nixpkgs.legacyPackages.${system};
    in
    {
      devShells.${system}.default = pkgs.mkShell {
        packages = with pkgs; [
          go
          gopls
          gotools

          uv
          python313
        ];

        shellHook = ''
          export UV_PYTHON_DOWNLOADS=never
          export UV_PYTHON=${pkgs.python313}/bin/python  # Use Nix's Python
          export UV_NO_SYNC=1  # Skip uv's venv sync if using Nix's Python
          export LD_LIBRARY_PATH=${pkgs.lib.makeLibraryPath [
            pkgs.zlib
            pkgs.stdenv.cc.cc.lib
            # Add more as needed
          ]}:$LD_LIBRARY_PATH
        '';
      };
    };
}
