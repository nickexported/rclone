// These are in an external package because we need to import configfile
//
// Internal tests are in ui_internal_test.go

package config_test

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/configfile"
	"github.com/rclone/rclone/fs/config/obscure"
	"github.com/rclone/rclone/fs/rc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testConfigFile(t *testing.T, configFileName string) func() {
	ctx := context.Background()
	ci := fs.GetConfig(ctx)
	config.ClearConfigPassword()
	_ = os.Unsetenv("_RCLONE_CONFIG_KEY_FILE")
	_ = os.Unsetenv("RCLONE_CONFIG_PASS")
	// create temp config file
	tempFile, err := ioutil.TempFile("", configFileName)
	assert.NoError(t, err)
	path := tempFile.Name()
	assert.NoError(t, tempFile.Close())

	// temporarily adapt configuration
	oldOsStdout := os.Stdout
	oldConfigPath := config.GetConfigPath()
	oldConfig := *ci
	oldConfigFile := config.Data()
	oldReadLine := config.ReadLine
	oldPassword := config.Password
	os.Stdout = nil
	assert.NoError(t, config.SetConfigPath(path))
	ci = &fs.ConfigInfo{}

	configfile.Install()
	assert.Equal(t, []string{}, config.Data().GetSectionList())

	// Fake a remote
	fs.Register(&fs.RegInfo{
		Name: "config_test_remote",
		Options: fs.Options{
			{
				Name:       "bool",
				Default:    false,
				IsPassword: false,
			},
			{
				Name:       "pass",
				Default:    "",
				IsPassword: true,
			},
		},
	})

	// Undo the above
	return func() {
		err := os.Remove(path)
		assert.NoError(t, err)

		os.Stdout = oldOsStdout
		assert.NoError(t, config.SetConfigPath(oldConfigPath))
		config.ReadLine = oldReadLine
		config.Password = oldPassword
		*ci = oldConfig
		config.SetData(oldConfigFile)

		_ = os.Unsetenv("_RCLONE_CONFIG_KEY_FILE")
		_ = os.Unsetenv("RCLONE_CONFIG_PASS")
	}
}

// makeReadLine makes a simple readLine which returns a fixed list of
// strings
func makeReadLine(answers []string) func() string {
	i := 0
	return func() string {
		i = i + 1
		return answers[i-1]
	}
}

func TestCRUD(t *testing.T) {
	defer testConfigFile(t, "crud.conf")()
	ctx := context.Background()

	// script for creating remote
	config.ReadLine = makeReadLine([]string{
		"config_test_remote", // type
		"true",               // bool value
		"y",                  // type my own password
		"secret",             // password
		"secret",             // repeat
		"y",                  // looks good, save
	})
	require.NoError(t, config.NewRemote(ctx, "test"))

	assert.Equal(t, []string{"test"}, config.Data().GetSectionList())
	assert.Equal(t, "config_test_remote", config.FileGet("test", "type"))
	assert.Equal(t, "true", config.FileGet("test", "bool"))
	assert.Equal(t, "secret", obscure.MustReveal(config.FileGet("test", "pass")))

	// normal rename, test → asdf
	config.ReadLine = makeReadLine([]string{
		"asdf",
		"asdf",
		"asdf",
	})
	config.RenameRemote("test")

	assert.Equal(t, []string{"asdf"}, config.Data().GetSectionList())
	assert.Equal(t, "config_test_remote", config.FileGet("asdf", "type"))
	assert.Equal(t, "true", config.FileGet("asdf", "bool"))
	assert.Equal(t, "secret", obscure.MustReveal(config.FileGet("asdf", "pass")))

	// delete remote
	config.DeleteRemote("asdf")
	assert.Equal(t, []string{}, config.Data().GetSectionList())
}

func TestChooseOption(t *testing.T) {
	defer testConfigFile(t, "crud.conf")()
	ctx := context.Background()

	// script for creating remote
	config.ReadLine = makeReadLine([]string{
		"config_test_remote", // type
		"false",              // bool value
		"x",                  // bad choice
		"g",                  // generate password
		"1024",               // very big
		"y",                  // password OK
		"y",                  // looks good, save
	})
	config.Password = func(bits int) (string, error) {
		assert.Equal(t, 1024, bits)
		return "not very random password", nil
	}
	require.NoError(t, config.NewRemote(ctx, "test"))

	assert.Equal(t, "", config.FileGet("test", "bool")) // this is the default now
	assert.Equal(t, "not very random password", obscure.MustReveal(config.FileGet("test", "pass")))

	// script for creating remote
	config.ReadLine = makeReadLine([]string{
		"config_test_remote", // type
		"true",               // bool value
		"n",                  // not required
		"y",                  // looks good, save
	})
	require.NoError(t, config.NewRemote(ctx, "test"))

	assert.Equal(t, "true", config.FileGet("test", "bool"))
	assert.Equal(t, "", config.FileGet("test", "pass"))
}

func TestNewRemoteName(t *testing.T) {
	defer testConfigFile(t, "crud.conf")()
	ctx := context.Background()

	// script for creating remote
	config.ReadLine = makeReadLine([]string{
		"config_test_remote", // type
		"true",               // bool value
		"n",                  // not required
		"y",                  // looks good, save
	})
	require.NoError(t, config.NewRemote(ctx, "test"))

	config.ReadLine = makeReadLine([]string{
		"test",           // already exists
		"",               // empty string not allowed
		"bad@characters", // bad characters
		"newname",        // OK
	})

	assert.Equal(t, "newname", config.NewRemoteName())
}

func TestCreateUpdatePasswordRemote(t *testing.T) {
	ctx := context.Background()
	defer testConfigFile(t, "update.conf")()

	for _, doObscure := range []bool{false, true} {
		for _, noObscure := range []bool{false, true} {
			if doObscure && noObscure {
				break
			}
			t.Run(fmt.Sprintf("doObscure=%v,noObscure=%v", doObscure, noObscure), func(t *testing.T) {
				opt := config.UpdateRemoteOpt{
					Obscure:   doObscure,
					NoObscure: noObscure,
				}
				_, err := config.CreateRemote(ctx, "test2", "config_test_remote", rc.Params{
					"bool": true,
					"pass": "potato",
				}, opt)
				require.NoError(t, err)

				assert.Equal(t, []string{"test2"}, config.Data().GetSectionList())
				assert.Equal(t, "config_test_remote", config.FileGet("test2", "type"))
				assert.Equal(t, "true", config.FileGet("test2", "bool"))
				gotPw := config.FileGet("test2", "pass")
				if !noObscure {
					gotPw = obscure.MustReveal(gotPw)
				}
				assert.Equal(t, "potato", gotPw)

				wantPw := obscure.MustObscure("potato2")
				_, err = config.UpdateRemote(ctx, "test2", rc.Params{
					"bool":  false,
					"pass":  wantPw,
					"spare": "spare",
				}, opt)
				require.NoError(t, err)

				assert.Equal(t, []string{"test2"}, config.Data().GetSectionList())
				assert.Equal(t, "config_test_remote", config.FileGet("test2", "type"))
				assert.Equal(t, "false", config.FileGet("test2", "bool"))
				gotPw = config.FileGet("test2", "pass")
				if doObscure {
					gotPw = obscure.MustReveal(gotPw)
				}
				assert.Equal(t, wantPw, gotPw)

				require.NoError(t, config.PasswordRemote(ctx, "test2", rc.Params{
					"pass": "potato3",
				}))

				assert.Equal(t, []string{"test2"}, config.Data().GetSectionList())
				assert.Equal(t, "config_test_remote", config.FileGet("test2", "type"))
				assert.Equal(t, "false", config.FileGet("test2", "bool"))
				assert.Equal(t, "potato3", obscure.MustReveal(config.FileGet("test2", "pass")))
			})
		}
	}

}
