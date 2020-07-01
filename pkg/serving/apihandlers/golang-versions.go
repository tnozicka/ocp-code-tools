package apihandlers

var (
	releaseToGolangVersions = map[string]string{
		"4.1": "1.12",
		"4.2": "1.12",
		"4.3": "1.12",
		"4.4": "1.13",
		"4.5": "1.13",
		"4.6": "1.13",
	}
)

type GolangVersions struct {
}

func GetGolangVersions(configPath string) (*GolangVersions, error) {
	c, err := GetCIConfig(configPath)
	if err != nil {
		return nil, err
	}

}
