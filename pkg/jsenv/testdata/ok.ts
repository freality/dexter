import { defineDexConfig, service } from "@dex/iac";

defineDexConfig({ project: "test" });
service("whoami", { plugin: "docker", properties: { image: "traefik/whoami" } });
