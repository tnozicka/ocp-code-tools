package api

type RepositoryConfig struct {
	Name               string
	GithubOrganisation string
	GithubRepository   string
	GitURL             string
	GitRef             string
	GitSha             string
	ReleaseConfigPath  string
}

type ReleaseConfig struct {
	StreamName        string
	RepositoryConfigs []RepositoryConfig
}

type Config struct {
	GitURL   string
	GitSha   string
	Releases []ReleaseConfig
}
