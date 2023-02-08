package kubernetes

import (
	sentry_debug "github.com/dapr/dapr/code_debug/sentry"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	kauthapi "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fake "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
)

func TestValidate(t *testing.T) {
	t.Run("invalid token", func(t *testing.T) {
		fakeClient := &fake.Clientset{}
		fakeClient.Fake.AddReactor(
			"create",
			"tokenreviews",
			func(action core.Action) (bool, runtime.Object, error) {
				return true, &kauthapi.TokenReview{Status: kauthapi.TokenReviewStatus{Error: "bad token"}}, nil
			})

		v := validator{
			client: fakeClient,
			auth:   fakeClient.AuthenticationV1(),
		}

		err := v.Validate("a1:ns1", "a2:ns2", "ns2")
		assert.Equal(t, errors.Errorf("%s: invalid token: bad token", errPrefix).Error(), err.Error())
	})

	t.Run("unauthenticated", func(t *testing.T) {
		fakeClient := &fake.Clientset{}
		fakeClient.Fake.AddReactor(
			"create",
			"tokenreviews",
			func(action core.Action) (bool, runtime.Object, error) {
				return true, &kauthapi.TokenReview{Status: kauthapi.TokenReviewStatus{Authenticated: false}}, nil
			})

		v := validator{
			client: fakeClient,
			auth:   fakeClient.AuthenticationV1(),
		}

		err := v.Validate("a1:ns1", "a2:ns2", "ns")
		expectedErr := errors.Errorf("%s: authentication failed", errPrefix)
		assert.Equal(t, expectedErr.Error(), err.Error())
	})

	t.Run("bad token structure", func(t *testing.T) {
		fakeClient := &fake.Clientset{}
		fakeClient.Fake.AddReactor(
			"create",
			"tokenreviews",
			func(action core.Action) (bool, runtime.Object, error) {
				return true, &kauthapi.TokenReview{Status: kauthapi.TokenReviewStatus{Authenticated: true, User: kauthapi.UserInfo{Username: "name"}}}, nil
			})

		v := validator{
			client: fakeClient,
			auth:   fakeClient.AuthenticationV1(),
		}

		err := v.Validate("a1:ns1", "a2:ns2", "ns2")
		expectedErr := errors.Errorf("%s: provided token is not a properly structured service account token", errPrefix)
		assert.Equal(t, expectedErr.Error(), err.Error())
	})

	t.Run("token id mismatch", func(t *testing.T) {
		fakeClient := &fake.Clientset{}
		fakeClient.Fake.AddReactor(
			"create",
			"tokenreviews",
			func(action core.Action) (bool, runtime.Object, error) {
				return true, &kauthapi.TokenReview{Status: kauthapi.TokenReviewStatus{Authenticated: true, User: kauthapi.UserInfo{Username: "system:serviceaccount:ns1:a1"}}}, nil
			})

		v := validator{
			client: fakeClient,
			auth:   fakeClient.AuthenticationV1(),
		}

		err := v.Validate("ns2:a1", "ns2:a2", "ns1")
		expectedErr := errors.Errorf("%s: token/id mismatch. received id: ns2:a1", errPrefix)
		assert.Equal(t, expectedErr.Error(), err.Error())
	})

	t.Run("empty token", func(t *testing.T) {
		fakeClient := fake.NewSimpleClientset()
		v := validator{
			client: fakeClient,
			auth:   fakeClient.AuthenticationV1(),
		}

		err := v.Validate("a1:ns1", "", "ns")
		expectedErr := errors.Errorf("%s: token field in request must not be empty", errPrefix)
		assert.Equal(t, expectedErr.Error(), err.Error())
	})

	t.Run("empty id", func(t *testing.T) {
		fakeClient := fake.NewSimpleClientset()
		v := validator{
			client: fakeClient,
			auth:   fakeClient.AuthenticationV1(),
		}

		err := v.Validate("", "a1:ns1", "ns")
		expectedErr := errors.Errorf("%s: id field in request must not be empty", errPrefix)
		assert.Equal(t, expectedErr.Error(), err.Error())
	})

	t.Run("valid authentication", func(t *testing.T) {
		fakeClient := &fake.Clientset{}
		fakeClient.Fake.AddReactor(
			"create",
			"tokenreviews",
			func(action core.Action) (bool, runtime.Object, error) {
				return true, &kauthapi.TokenReview{Status: kauthapi.TokenReviewStatus{Authenticated: true, User: kauthapi.UserInfo{Username: "system:serviceaccount:ns1:a1"}}}, nil
			})

		v := validator{
			client: fakeClient,
			auth:   fakeClient.AuthenticationV1(),
		}

		err := v.Validate("ns1:a1", "ns1:a1", "ns1")
		assert.NoError(t, err)
	})

	t.Run("myself", func(t *testing.T) {
		println(NewValidator(sentry_debug.GetK8s()).Validate(
			`dp-61b7fa0d5c5ca0f638670680-executorapp-4f9b5-787779868f-krfxp`,
			`eyJhbGciOiJSUzI1NiIsImtpZCI6IkE3UktoWU8yU2N5YTRMak9seTFHNHVSbGZvd0xlVXlSZDN1OF9NVDVOVmMifQ.eyJhdWQiOlsiaHR0cHM6Ly9rdWJlcm5ldGVzLmRlZmF1bHQuc3ZjLmNsdXN0ZXIubG9jYWwiXSwiZXhwIjoxNjcwOTgzMDU2LCJpYXQiOjE2Mzk0NDcwNTYsImlzcyI6Imh0dHBzOi8va3ViZXJuZXRlcy5kZWZhdWx0LnN2Yy5jbHVzdGVyLmxvY2FsIiwia3ViZXJuZXRlcy5pbyI6eyJuYW1lc3BhY2UiOiJtZXNvaWQiLCJwb2QiOnsibmFtZSI6ImRwLTYxYjdmYTBkNWM1Y2EwZjYzODY3MDY4MC1leGVjdXRvcmFwcC00ZjliNS03ODc3Nzk4NjhmLWtyZnhwIiwidWlkIjoiMzRlY2M1MmEtNDdlMC00YzNmLThlZDktM2NjN2EzZTYwMmEyIn0sInNlcnZpY2VhY2NvdW50Ijp7Im5hbWUiOiJkZWZhdWx0IiwidWlkIjoiN2Q3YTZlYWUtOWYxZS00ODJmLTk2YjgtYjdlMmZlNDA3NDc5In0sIndhcm5hZnRlciI6MTYzOTQ1MDY2M30sIm5iZiI6MTYzOTQ0NzA1Niwic3ViIjoic3lzdGVtOnNlcnZpY2VhY2NvdW50Om1lc29pZDpkZWZhdWx0In0.xC0YNeahfbPcBCzslg93Sr6DKZceeNlX3OHwc_1Pap65OYVe4Zzu1nZsAE66WLQps4VjYnt5lQsGLJQdcc2gAeUv_Ju7MM5nIkHbjjQgN1OLh3OqhE8b4UfLxEmcF8SZrQPLcHgDO25XwExbi7tDQ_uyV90FA48WWao6KOwFFnOfoF1rghkbWQGyzYRtRvCNEFktsaWOocwQo9Tz6SECAL0mvKYn2gGMgWCl-q06T7DZu-n4FOjPVQ8mZpHxb-MWYD_hr1ZaI2LNW5YxehUfiO8Q795kBiVUR17y_IkqudzEoK5YAK4BoVYbU1GhFDznsDbPR2zYbtlWGjx9hBqT7A`,
			`mesoid`,
		).Error())
	})
}
