package activation

import "time"

// logStampCT formats t the way the bot's logger stamps a log line: in
// America/Chicago (logger.CTLocation), never the machine zone. lineTime parses
// the stamp in CT (release #22 P3-3), so a fixture written in the machine zone
// was 5 h off on a UTC CI runner and the watch tests went red there only.
func logStampCT(t time.Time) string {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		panic(err)
	}
	return t.In(loc).Format("01-02 15:04:05")
}
