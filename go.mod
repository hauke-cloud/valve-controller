module github.com/hauke-cloud/iot/valve-controller

go 1.22

require (
	github.com/eclipse/paho.mqtt.golang v1.4.3
	github.com/go-chi/chi/v5 v5.1.0
	github.com/go-playground/validator/v10 v10.22.0
	github.com/golang-migrate/migrate/v4 v4.18.1
	github.com/jackc/pgx/v5 v5.6.0
	github.com/prometheus/client_golang v1.19.1
	k8s.io/api v0.30.6
	k8s.io/apimachinery v0.30.6
	k8s.io/client-go v0.30.6
	sigs.k8s.io/controller-runtime v0.18.4
)
