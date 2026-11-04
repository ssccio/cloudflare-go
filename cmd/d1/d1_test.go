package d1

import "testing"

func TestCheckReadOnly(t *testing.T) {
	allow := []string{
		"SELECT max(processed_at) FROM processed_objects",
		"select count(*) from routes; select 1",
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"SELECT created_at, updated_at FROM audit_log", // column names that contain keywords
	}
	for _, s := range allow {
		if err := checkReadOnly(s); err != nil {
			t.Errorf("checkReadOnly(%q) = %v, want nil", s, err)
		}
	}
	refuse := []string{
		"",
		"DELETE FROM routes",
		"select 1; drop table routes",
		"WITH x AS (SELECT 1) DELETE FROM routes",
		"PRAGMA journal_mode=delete",
		"insert into t values (1)",
	}
	for _, s := range refuse {
		if err := checkReadOnly(s); err == nil {
			t.Errorf("checkReadOnly(%q) = nil, want refusal", s)
		}
	}
}
