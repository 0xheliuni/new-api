package dto

import "testing"

func TestResolveAssetProvider(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty defaults to byteplus", "", AssetProviderBytePlus},
		{"explicit byteplus", "byteplus", AssetProviderBytePlus},
		{"explicit cloudwise", "cloudwise", AssetProviderCloudwise},
		{"unknown falls back to byteplus", "wat", AssetProviderBytePlus},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := ChannelOtherSettings{AssetProvider: tc.in}
			if got := s.ResolveAssetProvider(); got != tc.want {
				t.Errorf("ResolveAssetProvider() = %q, want %q", got, tc.want)
			}
		})
	}
}
