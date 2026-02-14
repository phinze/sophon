{ lib, buildGoModule, lastModifiedDate ? "19700101000000" }:

buildGoModule {
  pname = "sophon";
  version = "0.1.0-${builtins.substring 0 12 lastModifiedDate}";

  src = builtins.path {
    path = ./..;
    name = "sophon-source";
  };

  vendorHash = null; # No external dependencies

  env.CGO_ENABLED = "0";

  meta = with lib; {
    description = "Notification + response relay for Claude Code sessions";
    homepage = "https://github.com/phinze/sophon";
    license = licenses.asl20;
    maintainers = [ ];
    mainProgram = "sophon";
  };
}
