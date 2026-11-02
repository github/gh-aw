package workflow

var objectPackLimitEnvNames = []string{
	"GH_AW_OBJECT_PACK_MAX_BYTES",
	"GH_AW_OBJECT_PACK_MAX_OBJECTS",
	"GH_AW_OBJECT_PACK_TIMEOUT_MS",
	"GH_AW_OBJECT_PACK_FETCH_DEPTHS",
}

func addObjectPackLimitEnv(env map[string]string) {
	for _, name := range objectPackLimitEnvNames {
		env[name] = "${{ vars." + name + " }}"
	}
}
