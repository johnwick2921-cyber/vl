package updaterworker

import (
	"errors"
	"testing"
)

// PIN: the worker refuses to run as root (a root-owned data/updater would lock
// the bot into a hold it cannot read), inside the bot's control group (the
// unit's KillMode=control-group would kill the worker with the bot, mid-job),
// and with TZ set (log line times are the bot's local zone). CheckProcess is
// what `vl-updater serve` calls first (cmd TestEverySubcommandRefusesRoot).
func TestWorkerRefusesRoot(t *testing.T) {
	origE, origC, origT := geteuid, readCgroup, lookupTZ
	t.Cleanup(func() { geteuid, readCgroup, lookupTZ = origE, origC, origT })
	user := func() int { return 1000 }
	plainCgroup := func() ([]byte, error) { return []byte("0::/user.slice/user-1000.slice/session-3.scope\n"), nil }
	noTZ := func() (string, bool) { return "", false }
	for name, c := range map[string]struct {
		euid   func() int
		cgroup func() ([]byte, error)
		tz     func() (string, bool)
		want   error
	}{
		"an ordinary user":    {user, plainCgroup, noTZ, nil},
		"root":                {func() int { return 0 }, plainCgroup, noTZ, ErrRoot},
		"inside nofx.service": {user, func() ([]byte, error) { return []byte("0::/system.slice/nofx.service\n"), nil }, noTZ, ErrBotCgroup}, "inside vl.service": {user, func() ([]byte, error) { return []byte("0::/system.slice/vl.service\n"), nil }, noTZ, ErrBotCgroup}, // R5 removes the nofx name, keeps vl		"TZ set":                    {user, plainCgroup, func() (string, bool) { return "America/Chicago", true }, ErrTZ},
		"TZ set empty is still set": {user, plainCgroup, func() (string, bool) { return "", true }, ErrTZ},
	} {
		geteuid, readCgroup, lookupTZ = c.euid, c.cgroup, c.tz
		if err := CheckProcess(); !errors.Is(err, c.want) || (c.want == nil && err != nil) {
			t.Errorf("%s: CheckProcess = %v, want %v", name, err, c.want)
		}
	}
}
