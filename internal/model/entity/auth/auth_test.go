package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAlibabaCloudConfig_Marshal(t *testing.T) {
	v := new(AlibabaCloudConfig)
	t.Log(v.Marshal())
}

func TestAlibabaCloudConfig_Unmarshal(t *testing.T) {

	cfg := AlibabaCloudConfig{
		Access:       "AccessKeyId",
		Secret:       "AccessKeySecret",
		SignName:     "SignName",
		Endpoint:     "Endpoint",
		TemplateCode: "VerifyTemplateCode",
	}
	data := cfg.Marshal()
	v := new(AlibabaCloudConfig)
	err := v.Unmarshal(data)
	if err != nil {
		t.Fatal(err.Error())
	}
	assert.Equal(t, "AccessKeyId", v.Access)
}
