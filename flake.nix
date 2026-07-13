{
  description = "SkySkins";

  inputs.nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";

  outputs = {nixpkgs, ...}: let
    eachSystem = nixpkgs.lib.genAttrs nixpkgs.lib.systems.flakeExposed;
  in {
    devShells = eachSystem (
      system: let
        pkgs = import nixpkgs {
          inherit system;
        };
      in {
        default = pkgs.mkShell {
          PGDATA = "./.nix/postgres";
          PGPORT = "5432";

          shellHook = ''
            mkdir -p "$PGDATA" && [ ! -f "$PGDATA/PG_VERSION" ] && initdb -D "$PGDATA" --auth=trust > /dev/null 2>&1

            if [ -f "$PGDATA/PG_VERSION" ]; then
              if pg_isready -h 127.0.0.1 -p $PGPORT > /dev/null 2>&1; then
                createdb -h 127.0.0.1 -p $PGPORT -U "$USER" skyskins 2>/dev/null || true
              else
                pg_ctl -D "$PGDATA" -o "-p $PGPORT -k /tmp" -l "$PGDATA/pg.log" start > /dev/null 2>&1
                until pg_isready -h 127.0.0.1 -p $PGPORT > /dev/null 2>&1; do sleep 0.5; done
                createdb -h 127.0.0.1 -p $PGPORT -U "$USER" skyskins 2>/dev/null || true
                pg_ctl -D "$PGDATA" stop > /dev/null 2>&1
              fi
            fi
          '';

          packages = with pkgs; [
            process-compose
            postgresql
            go
          ];
        };
      }
    );
  };
}
