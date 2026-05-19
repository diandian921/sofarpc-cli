package mcp

import "github.com/diandian921/sofarpc-cli/cli/internal/appconfig"

func loadConfig() (appconfig.Config, error) {
	path, err := appconfig.DefaultPath()
	if err != nil {
		return appconfig.Config{}, err
	}
	return appconfig.Load(path)
}

func configPaths() (string, string, error) {
	path, err := appconfig.DefaultPath()
	if err != nil {
		return "", "", err
	}
	lock, err := appconfig.DefaultLockPath()
	if err != nil {
		return "", "", err
	}
	return path, lock, nil
}

func mutateOnly(path, lock string, mutate func(*appconfig.Config) error) error {
	_, err := appconfig.Update(path, lock, mutate)
	return err
}
