{
  lib,
  buildGoModule,
  buildNpmPackage,
  lastModifiedDate ? "19700101000000",
}:

let
  src = builtins.path {
    path = ./..;
    name = "sophon-source";
  };

  frontend = buildNpmPackage {
    pname = "sophon-frontend";
    version = "0.1.0";
    inherit src;
    sourceRoot = "sophon-source/server/frontend";
    npmDepsHash = "sha256-8SoYDeQSllo/1bGQgl3iiJfZ/dviQFYVuVzpOJM3L50=";
    dontNpmBuild = true;
    buildPhase = ''
      npx esbuild src/app.ts src/style.css \
        --bundle --outdir=$out --minify --target=es2020
    '';
    installPhase = ''
      # output already in $out from buildPhase
    '';
  };
in

buildGoModule {
  pname = "sophon";
  version = "0.1.0-${builtins.substring 0 12 lastModifiedDate}";

  inherit src;

  vendorHash = "sha256-h6cHghxBPGqLh80r5q8zipjBOUZdtbPpGlVEH/AYvhI=";

  env.CGO_ENABLED = "0";

  preBuild = ''
    cp ${frontend}/app.js server/static/
    cp ${frontend}/style.css server/static/
  '';

  meta = with lib; {
    description = "Notification + response relay for Claude Code sessions";
    homepage = "https://github.com/phinze/sophon";
    license = licenses.asl20;
    maintainers = [ ];
    mainProgram = "sophon";
  };
}
