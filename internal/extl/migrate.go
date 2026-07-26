package extl

import (
	"os"
	"path/filepath"
)

type moduleMigrator struct {
	initialBasePath string
	finalBasePath   string
}

func (m *moduleMigrator) moduleMigratorHard(fileName string) error {
	clean, err := cleanModuleRelativePath(fileName)
	if err != nil {
		return err
	}
	source, err := os.OpenRoot(m.initialBasePath)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	buf, err := source.ReadFile(clean)
	if err != nil {
		return err
	}

	destination, err := os.OpenRoot(m.finalBasePath)
	if err != nil {
		return err
	}
	defer func() { _ = destination.Close() }()
	if err := destination.MkdirAll(filepath.Dir(clean), 0o755); err != nil {
		return err
	}
	return destination.WriteFile(clean, buf, 0o644)
}

// func (m *moduleMigrator) moduleMigratorSoft(fileName string) error {
// 	iPath := filepath.Join(m.initialBasePath, fileName)
// 	fPath := filepath.Join(m.finalBasePath, fileName)
// 	return os.Rename(iPath, fPath)
// }

// func migrateManifestFile(migrator *moduleMigrator) (moduleMigratorFunc, error) {
// 	// Try soft migration firstly
// 	err := migrator.moduleMigratorSoft("manifest.json")
// 	if err == nil {
// 		return migrator.moduleMigratorSoft, nil
// 	}
// 	// If it fails, try hard migration
// 	err = migrator.moduleMigratorHard("manifest.json")
// 	if err == nil {
// 		return migrator.moduleMigratorHard, nil
// 	}
// 	// If hard migration also fails, return error
// 	return nil, err
// }

func migrateModule(m *Module, hash, path string) error {
	// Generate a module hash
	if hash == "" {
		hash = generateHash(DEF_MODULE_HASH)
	}
	if !validModuleID(hash) {
		return ErrPathOutsideModule
	}

	// Create a migrator instance
	migrator := moduleMigrator{
		initialBasePath: m.modulePath,
		finalBasePath:   filepath.Join(path, hash),
	}

	err := os.MkdirAll(migrator.finalBasePath, 0o755)
	if err != nil {
		return err
	}

	flush := func(err error) error {
		_ = os.RemoveAll(migrator.finalBasePath)
		return err
	}
	err = migrator.moduleMigratorHard("manifest.json")
	if err != nil {
		return flush(err)
	}
	// Migrate entrypoint file
	err = migrator.moduleMigratorHard(m.Entrypoint)
	if err != nil {
		return flush(err)
	}
	// Migrate all the required files (imported js modules)
	for _, modName := range m.runtime.imported {
		err = migrator.moduleMigratorHard(modName)
		if err != nil {
			return flush(err)
		}
	}
	// Migrate all the assets
	for _, assetName := range m.Assets {
		err = migrator.moduleMigratorHard(assetName)
		if err != nil {
			return flush(err)
		}
	}
	nM, err := OpenModule(m.l, migrator.finalBasePath)
	if err != nil {
		return flush(err)
	}
	err = nM.Load()
	if err != nil {
		return flush(err)
	}
	nM.ModuleId = hash
	*m = *nM
	return nil
}
