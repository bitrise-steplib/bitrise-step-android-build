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

// Configs ...
type Configs struct {
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
	var config Configs

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

	if len(apks) == 0 {
		log.Warnf("No apks found with pattern: %s", config.APKPathPattern)
		log.Warnf("If you have changed default APK export path in your gradle files then you might need to change APKPathPattern accordingly.")
		os.Exit(0)
	}

	var exportedArtifactPaths []string

	for _, apk := range apks {
		exists, err := pathutil.IsPathExists(filepath.Join(deployDir, apk.Name))
		if err != nil {
			failf("failed to check path, error: %v", err)
		}

		artifactName := filepath.Base(apk.Path)

		if exists {
			timestamp := time.Now().Format("20060102150405")
			ext := filepath.Ext(apk.Name)
			name := strings.TrimSuffix(filepath.Base(apk.Name), ext)
			apk.Name = fmt.Sprintf("%s-%s%s", name, timestamp, ext)
		}

		log.Printf("  Export [ %s => $BITRISE_DEPLOY_DIR/%s ]", artifactName, apk.Name)

		if err := apk.Export(deployDir); err != nil {
			log.Warnf("failed to export apk (%s), error: %v", apk.Path, err)
			continue
		}

		exportedArtifactPaths = append(exportedArtifactPaths, filepath.Join(deployDir, apk.Name))
	}

	if len(exportedArtifactPaths) == 0 {
		failf("Could not export any APKs")
	}

	lastExportedArtifact := exportedArtifactPaths[len(exportedArtifactPaths)-1]

	fmt.Println()
	if err := tools.ExportEnvironmentWithEnvman(apkEnvKey, lastExportedArtifact); err != nil {
		failf("Failed to export environment variable: %s", apkEnvKey)
	}
	log.Printf("  Env    [ $%s = $BITRISE_DEPLOY_DIR/%s ]", apkEnvKey, filepath.Base(lastExportedArtifact))

	var paths, sep string
	for _, path := range exportedArtifactPaths {
		paths += sep + "$BITRISE_DEPLOY_DIR/" + filepath.Base(path)
		sep = "| \\\n" + strings.Repeat(" ", 11)
	}

	if err := tools.ExportEnvironmentWithEnvman(apkEnvKey, lastExportedArtifact); err != nil {
		failf("Failed to export environment variable: %s", apkEnvKey)
	}
	log.Printf("  Env    [ $%s = %s ]", apkListEnvKey, paths)

	fmt.Println()

	log.Infof("Export mapping files:")
	fmt.Println()

	mappings, err := gradleProject.FindArtifacts(started, mappingFilePattern, true)
	if err != nil {
		failf("Failed to find mapping files, error: %v", err)
	}

	if len(mappings) == 0 {
		log.Printf("No mapping files found with pattern: %s", mappingFilePattern)
		log.Printf("You might have changed default mapping file export path in your gradle files or obfuscation is not enabled in your project.")
		os.Exit(0)
	}

	for _, mapping := range mappings {
		exists, err := pathutil.IsPathExists(filepath.Join(deployDir, mapping.Name))
		if err != nil {
			failf("failed to check path, error: %v", err)
		}

		artifactName := filepath.Base(mapping.Path)

		if exists {
			timestamp := time.Now().Format("20060102150405")
			ext := filepath.Ext(mapping.Name)
			name := strings.TrimSuffix(filepath.Base(mapping.Name), ext)
			mapping.Name = fmt.Sprintf("%s-%s%s", name, timestamp, ext)
		}

		log.Printf("  Export [ %s => $BITRISE_DEPLOY_DIR/%s ]", artifactName, mapping.Name)

		if err := mapping.Export(deployDir); err != nil {
			log.Warnf("failed to export mapping.txt (%s), error: %v", mapping.Path, err)
			continue
		}

		lastExportedArtifact = filepath.Join(deployDir, mapping.Name)
	}

	fmt.Println()
	if err := tools.ExportEnvironmentWithEnvman(mappingFileEnvKey, lastExportedArtifact); err != nil {
		failf("Failed to export environment variable: %s", mappingFileEnvKey)
	}
	log.Printf("  Env    [ $%s = $BITRISE_DEPLOY_DIR/%s ]", mappingFileEnvKey, filepath.Base(lastExportedArtifact))
}
