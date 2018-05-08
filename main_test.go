package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bitrise-tools/go-android/gradle"
)

func TestFilterVariants(t *testing.T) {
	variants := gradle.Variants{
		"module1": []string{"variant1", "variant2", "variant3", "variant4", "variant5", "shared"},
		"module2": []string{"2variant1", "2variant2", "shared", "2variant3", "2variant4", "2variant5"},
	}

	t.Log("exact match for module and variant")
	{
		filtered, err := filterVariants("module1", "variant3", variants)
		require.NoError(t, err)

		expectedVariants := gradle.Variants{
			"module1": []string{"variant3"},
		}

		require.Equal(t, expectedVariants, filtered)

		filtered, err = filterVariants("module1", "variant100", variants)
		require.Error(t, err)

		filtered, err = filterVariants("module100", "variant100", variants)
		require.Error(t, err)

		filtered, err = filterVariants("module100", "variant1", variants)
		require.Error(t, err)
	}

	t.Log("exact match for module")
	{
		filtered, err := filterVariants("module1", "", variants)
		require.NoError(t, err)

		expectedVariants := gradle.Variants{
			"module1": []string{"variant1", "variant2", "variant3", "variant4", "variant5", "shared"},
		}

		require.Equal(t, expectedVariants, filtered)

		filtered, err = filterVariants("module3", "", variants)
		require.Error(t, err)
	}

	t.Log("exact match for variant")
	{
		filtered, err := filterVariants("", "variant2", variants)
		require.NoError(t, err)

		expectedVariants := gradle.Variants{
			"module1": []string{"variant2"},
		}

		require.Equal(t, expectedVariants, filtered)

		filtered, err = filterVariants("", "", variants)
		require.NoError(t, err)
		require.Equal(t, variants, filtered)

		filtered, err = filterVariants("", "shared", variants)
		require.NoError(t, err)

		expectedVariants = gradle.Variants{
			"module1": []string{"shared"},
			"module2": []string{"shared"},
		}

		require.Equal(t, expectedVariants, filtered)
	}
}
