package newapi

import "testing"

func TestValidateReadOnlyGrants(t *testing.T) {
	tests := []struct {
		name   string
		grants []string
		want   bool
	}{
		{name: "select only", grants: []string{"GRANT USAGE ON *.* TO 'pulse'@'%'", "GRANT SELECT ON `new_api`.`logs` TO 'pulse'@'%'"}, want: true},
		{name: "show view is read only", grants: []string{"GRANT SELECT, SHOW VIEW ON `new_api`.* TO 'pulse'@'%'"}, want: true},
		{name: "write privilege", grants: []string{"GRANT SELECT, INSERT ON `new_api`.`logs` TO 'pulse'@'%'"}, want: false},
		{name: "grant option", grants: []string{"GRANT SELECT ON `new_api`.`logs` TO 'pulse'@'%' WITH GRANT OPTION"}, want: false},
		{name: "all privileges", grants: []string{"GRANT ALL PRIVILEGES ON `new_api`.* TO 'pulse'@'%'"}, want: false},
		{name: "role grant is fail closed", grants: []string{"GRANT `new-api-reader`@`%` TO 'pulse'@'%'"}, want: false},
		{name: "empty", grants: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateReadOnlyGrants(tt.grants); got != tt.want {
				t.Fatalf("validateReadOnlyGrants(%v) = %v, want %v", tt.grants, got, tt.want)
			}
		})
	}
}
