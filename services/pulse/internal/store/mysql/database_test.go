package mysql

import "testing"

func TestPoolConfiguration(t *testing.T) {
	for _, key := range []string{"PULSE_DB_MAX_OPEN_CONNS", "PULSE_DB_MAX_IDLE_CONNS", "PULSE_DB_CONN_MAX_LIFETIME", "PULSE_DB_CONN_MAX_IDLE_TIME"} {
		t.Setenv(key, "")
	}
	t.Setenv("PULSE_DB_MAX_OPEN_CONNS", "4")
	t.Setenv("PULSE_DB_MAX_IDLE_CONNS", "2")
	p, err := poolOptionsFromEnv()
	if err != nil || p.open != 4 || p.idle != 2 {
		t.Fatalf("pool: %+v %v", p, err)
	}
	t.Setenv("PULSE_DB_MAX_IDLE_CONNS", "5")
	if _, err = poolOptionsFromEnv(); err == nil {
		t.Fatal("idle > max accepted")
	}
	t.Setenv("PULSE_DB_MAX_IDLE_CONNS", "2")
	t.Setenv("PULSE_DB_CONN_MAX_LIFETIME", "bad")
	if _, err = poolOptionsFromEnv(); err == nil {
		t.Fatal("bad lifetime accepted")
	}
}
