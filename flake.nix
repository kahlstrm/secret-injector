{
  description = "Build and development environment for secret-injector";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      ...
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        version = "0.0.0-dev";
        commit = self.shortRev or "dirty";
        basePackage = pkgs.buildGoModule {
          pname = "secret-injector";
          inherit version;
          src = ./.;

          goSum = ./go.sum;
          vendorHash = "sha256-267OsWd4QeRuzwTfEKGSqGw+QEVLi9BQLeecTxfrly8=";
          subPackages = [ "cmd/secret-injector" ];
          doCheck = true;
          env.CGO_ENABLED = 0;

          checkPhase = ''
            runHook preCheck
            go test ./...
            runHook postCheck
          '';

          ldflags = [
            "-s"
            "-w"
            "-X main.version=${version}"
            "-X main.commit=${commit}"
            "-X main.date=unknown"
          ];

          meta = {
            description = "Load secrets from cloud providers into environment variables";
            homepage = "https://github.com/kahlstrm/secret-injector";
            license = pkgs.lib.licenses.mit;
            mainProgram = "secret-injector";
          };
        };
        package = basePackage.overrideAttrs (oldAttrs: {
          env = (oldAttrs.env or { }) // {
            GOFLAGS = "${oldAttrs.env.GOFLAGS or ""} -gcflags=all=-l";
          };
        });
      in
      {
        packages.default = package;

        checks = {
          package = package;
          smoke = pkgs.runCommand "secret-injector-package-smoke" { } ''
            expected='secret-injector version ${version} (commit=${commit} date=unknown)'
            actual="$(${package}/bin/secret-injector --version)"
            test "$actual" = "$expected"

            ${package}/bin/secret-injector validate \
              --config '{"secrets":{"TEST":{"source":"aws_ssm","ref":"/test"}}}'

            actual="$(${package}/bin/secret-injector fetch \
              --config '{"secrets":{}}' \
              --format=json)"
            test "$actual" = '{}'

            touch "$out"
          '';
        };

        devShells.default = import ./shell.nix { inherit pkgs; };
      }
    );
}
