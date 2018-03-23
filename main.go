package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-utils/sliceutil"
	"github.com/bitrise-tools/go-android/gradle"
	"github.com/bitrise-tools/go-steputils/stepconf"
	"github.com/bitrise-tools/go-steputils/tools"
)

const (
	apkEnvKey          = "BITRISE_APK_PATH"
	apkListEnvKey      = "BITRISE_APK_PATH_LIST"
	mappingFileEnvKey  = "BITRISE_MAPPING_PATH"
	mappingFilePattern = "*build/*/mapping.txt"
)

// Config ...
type Config struct {
	ProjectLocation string `env:"project_location,required"`
	APKPathPattern  string `env:"apk_path_pattern"`
	Variant         string `env:"variant"`
	Module          string `env:"module"`
}

func failf(f string, args ...interface{}) {
	log.Errorf(f, args...)
	os.Exit(1)
}

func main() {
	var config Config

	if err := stepconf.Parse(&config); err != nil {
		failf("Couldn't create step config: %v\n", err)
	}

	stepconf.Print(config)

	deployDir := os.Getenv("BITRISE_DEPLOY_DIR")

	log.Printf("- Deploy dir: %s", deployDir)
	fmt.Println()

	gradleProject, err := gradle.NewProject(config.ProjectLocation)
	if err != nil {
		failf("Failed to open project, error: %s", err)
	}

	buildTask := gradleProject.
		GetModule(config.Module).
		GetTask("assemble")

	log.Infof("Variants:")
	fmt.Println()

	variants, err := buildTask.GetVariants()
	if err != nil {
		failf("Failed to fetch variants, error: %s", err)
	}

	filteredVariants := variants.Filter(config.Variant)

	for _, variant := range variants {
		if sliceutil.IsStringInSlice(variant, filteredVariants) {
			log.Donef("✓ %s", variant)
		} else {
			log.Printf("- %s", variant)
		}
	}

	fmt.Println()

	if len(filteredVariants) == 0 {
		errMsg := fmt.Sprintf("No variant matching for: (%s)", config.Variant)
		if config.Module != "" {
			errMsg += fmt.Sprintf(" in module: [%s]", config.Module)
		}
		failf(errMsg)
	}

	if config.Variant == "" {
		log.Warnf("No variant specified, build will run on all variants")
		fmt.Println()
	}

	started := time.Now()

	log.Infof("Run build:")
	if err := buildTask.Run(filteredVariants); err != nil {
		failf("Build task failed, error: %v", err)
	}
	fmt.Println()

	log.Infof("Export APKs:")
	fmt.Println()

	apks, err := gradleProject.FindArtifacts(started, config.APKPathPattern, false)
	if err != nil {
		failf("failed to find apks, error: %v", err)
	}

	if len(apks) > 0 {
		var lastExportedArtifact string
		var exportedArtifactPaths []string

		for _, artifact := range apks {
			exists, err := pathutil.IsPathExists(
				filepath.Join(deployDir, artifact.Name),
			)
			if err != nil {
				failf("failed to check path, error: %v", err)
			}

			artifactName := filepath.Base(artifact.Path)

			if exists {
				timestamp := time.Now().
					Format("20060102150405")
				ext := filepath.Ext(artifact.Name)
				name := strings.TrimSuffix(filepath.Base(artifact.Name), ext)
				artifact.Name = fmt.Sprintf("%s-%s%s", name, timestamp, ext)
			}

			log.Printf("  Export [ %s => $BITRISE_DEPLOY_DIR/%s ]", artifactName, artifact.Name)

			if err := artifact.Export(deployDir); err != nil {
				log.Warnf("failed to export apks, error: %v", err)
			}

			exportedPath := filepath.Join(deployDir, artifact.Name)

			lastExportedArtifact = exportedPath

			exportedArtifactPaths = append(exportedArtifactPaths, exportedPath)
		}

		fmt.Println()
		if err := tools.ExportEnvironmentWithEnvman(apkEnvKey, lastExportedArtifact); err != nil {
			failf("Failed to export environment variable: %s", apkEnvKey)
		}
		log.Printf("  Env    [ $%s = $BITRISE_DEPLOY_DIR/%s ]", apkEnvKey, filepath.Base(lastExportedArtifact))

		var pathListStr string
		for i, pth := range exportedArtifactPaths {
			if len(exportedArtifactPaths) == 1 {
				pathListStr = "$BITRISE_DEPLOY_DIR/" + filepath.Base(pth)
			} else {
				pathListStr += "$BITRISE_DEPLOY_DIR/" + filepath.Base(pth)
				if i < len(exportedArtifactPaths)-1 {
					pathListStr += "|\n           "
				}
			}
		}
		if err := tools.ExportEnvironmentWithEnvman(apkEnvKey, lastExportedArtifact); err != nil {
			failf("Failed to export environment variable: %s", apkEnvKey)
		}
		log.Printf("  Env    [ $%s = %s ]", apkListEnvKey, pathListStr)

	} else {
		log.Warnf("No apks found with pattern: %s", config.APKPathPattern)
		log.Warnf("If you have changed default APK export path in your gradle files then you might need to change APKPathPattern accordingly.")
	}

	fmt.Println()

	log.Infof("Export mapping files:")
	fmt.Println()

	mappings, err := gradleProject.FindArtifacts(started, mappingFilePattern, true)
	if err != nil {
		failf("failed to find mapping files, error: %v", err)
	}

	if len(mappings) > 0 {
		var lastExportedArtifact string

		for _, artifact := range mappings {
			exists, err := pathutil.IsPathExists(
				filepath.Join(deployDir, artifact.Name),
			)
			if err != nil {
				failf("failed to check path, error: %v", err)
			}

			artifactName := filepath.Base(artifact.Path)

			if exists {
				timestamp := time.Now().
					Format("20060102150405")
				ext := filepath.Ext(artifact.Name)
				name := strings.TrimSuffix(filepath.Base(artifact.Name), ext)
				artifact.Name = fmt.Sprintf("%s-%s%s", name, timestamp, ext)
			}

			log.Printf("  Export [ %s => $BITRISE_DEPLOY_DIR/%s ]", artifactName, artifact.Name)

			if err := artifact.Export(deployDir); err != nil {
				log.Warnf("failed to export apks, error: %v", err)
			}

			lastExportedArtifact = filepath.Join(deployDir, artifact.Name)
		}

		fmt.Println()
		if err := tools.ExportEnvironmentWithEnvman(mappingFileEnvKey, lastExportedArtifact); err != nil {
			failf("Failed to export environment variable: %s", mappingFileEnvKey)
		}
		log.Printf("  Env    [ $%s = $BITRISE_DEPLOY_DIR/%s ]", mappingFileEnvKey, filepath.Base(lastExportedArtifact))
	} else {
		log.Printf("No mapping files found with pattern: %s", mappingFilePattern)
		log.Printf("You might have changed default mapping file export path in your gradle files or obfuscation is not enabled in your project.")
	}
}
