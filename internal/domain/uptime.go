package domain

type Uptime struct {
	Checks int64
	Up     int64
}

func (u Uptime) Ratio() (float64, bool) {
	if u.Checks == 0 {
		return 0, false
	}
	return float64(u.Up) / float64(u.Checks), true
}

type UptimeReport struct {
	Hour Uptime
	Day  Uptime
	Week Uptime
}
