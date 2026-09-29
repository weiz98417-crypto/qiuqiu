package auth

import "testing"

// 登录表单校验（ADR-0020 决定 4 + 2026-09 修订）：identifier 双形态、
// 密码下限 7（容纳演示账号 test/5055365）。
func TestValidateLoginRequest(t *testing.T) {
	cases := []struct {
		name       string
		identifier string
		password   string
		wantErr    bool
	}{
		{"邮箱形", "fan@example.com", "50553658", false},
		{"用户名形", "test", "5055365", false},
		{"用户名形带点", "qiu.fan", "50553658", false},
		{"用户名太短", "ab", "50553658", true},
		{"用户名带非法字符", "test fan", "50553658", true},
		{"密码七位整", "test", "5055365", false},
		{"密码六位", "test", "505536", true},
		{"密码过长", "test", string(make([]byte, 129)), true},
		{"空标识", "", "50553658", true},
		{"孤立 @", "@", "50553658", true},
		{"@ 结尾", "fan@", "50553658", true},
	}
	for _, tc := range cases {
		err := ValidateLoginRequest(tc.identifier, tc.password)
		if (err != nil) != tc.wantErr {
			t.Fatalf("%s: ValidateLoginRequest(%q, %q) = %v, wantErr %v", tc.name, tc.identifier, tc.password, err, tc.wantErr)
		}
	}
}
