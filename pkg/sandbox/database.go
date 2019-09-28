/*
Copyright (c) 2019 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// This file contains the implementation of the database type.

package sandbox

import (
	"database/sql"
	"fmt"
	"net/url"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/jhernand/sandbox/pkg/internal"
)

// Database represents a the PostgreSQL database.
type Database struct {
	// Reference to the sandbox that created this database:
	sb *Sandbox

	// Database connection details:
	user     string
	password string
	name     string
}

// Source returns the database connection string.
func (d *Database) Source() string {
	return d.sb.dbURL(d.user, d.password, d.sb.dbAddress, d.name, nil).String()
}

// Destroy deletes the database and the user associated to this database.
func (d *Database) Destroy() error {
	// Create a connection to the database server using the administrators credentials and use
	// it to drop the database and the user:
	dbAdminURL := d.sb.dbURL(
		d.sb.dbAdminUser,
		d.sb.dbAdminPassword,
		d.sb.dbAddress,
		dbAdminDatabase,
		nil,
	)
	dbAdminHandle, err := sql.Open(dbDriver, dbAdminURL.String())
	if err != nil {
		return err
	}
	dbAdminClose := func() {
		err := dbAdminHandle.Close()
		if err != nil {
			log.Errorf("Can't close database handle: %v", err)
		}
	}
	defer dbAdminClose()
	_, err = dbAdminHandle.Exec(fmt.Sprintf("DROP DATABASE %s", d.name))
	if err != nil {
		return err
	}
	_, err = dbAdminHandle.Exec(
		fmt.Sprintf("DROP USER %s", d.user),
	)
	if err != nil {
		return err
	}

	return nil
}

// Database creates a new user and database in the PostgreSQL server of the sandbox and returns
// an object that can be used to interact with it.
func (s *Sandbox) Database() (database *Database, err error) {
	// Make sure that the database exists:
	err = s.ensureDBServer()
	if err != nil {
		return
	}

	// Create a connection to the database server using the administrators credentials:
	dbAdminURL := s.dbURL(
		s.dbAdminUser,
		s.dbAdminPassword,
		s.dbAddress,
		dbAdminDatabase,
		nil,
	)
	dbAdminHandle, err := sql.Open(dbDriver, dbAdminURL.String())
	if err != nil {
		return
	}
	dbAdminClose := func() {
		err := dbAdminHandle.Close()
		if err != nil {
			log.Errorf("Can't close database handle: %v", err)
		}
	}
	defer dbAdminClose()

	// Create the user and database name using the sequence:
	var nextVal int
	err = dbAdminHandle.QueryRow("SELECT nextval('sandbox')").Scan(&nextVal)
	if err != nil {
		return
	}
	dbUser := fmt.Sprintf("sandbox%d", nextVal)
	dbName := fmt.Sprintf("sandbox%d", nextVal)

	// Create a random password:
	randomUUID, err := uuid.NewRandom()
	if err != nil {
		return
	}
	dbPassword := randomUUID.String()

	// Create the user and the database:
	_, err = dbAdminHandle.Exec(
		fmt.Sprintf("CREATE USER %s WITH PASSWORD '%s'", dbUser, dbPassword),
	)
	if err != nil {
		return
	}
	_, err = dbAdminHandle.Exec(
		fmt.Sprintf("CREATE DATABASE %s OWNER %s", dbName, dbUser),
	)
	if err != nil {
		return
	}

	// Create and populate the object:
	database = &Database{
		sb:       s,
		user:     dbUser,
		password: dbPassword,
		name:     dbName,
	}

	return
}

func (s *Sandbox) ensureDBServer() error {
	// Nothing to do if the database server is ready:
	if s.dbReady {
		return nil
	}

	// Make sure that the database administrator password has been generated:
	err := s.ensureDBCredentials()
	if err != nil {
		return err
	}

	// Generate the script that will be executed by the initialization container to configure
	// the PostgreSQL server:
	initScript, err := internal.Template(
		dbInitScriptTemplate,
		"TLSDir", dbTLSDir,
		"ConfigDir", dbConfigDir,
		"DataDir", dbDataDir,
	)
	if err != nil {
		return err
	}

	// Create the specifications of the volumes that will be used by the PostgreSQL server:
	tlsVolume := internal.SecretVolume("tls", dbTLSSecretName)
	configVolume := internal.EmptyDirVolume("config")
	dataVolume := internal.EmptyDirVolume("data")

	// Create the pod:
	podLabels := map[string]string{
		internal.AppLabel: dbApp,
	}
	podEnv := []corev1.EnvVar{
		internal.SecretEnvVar(
			"POSTGRESQL_ADMIN_PASSWORD",
			dbAdminSecretName,
			corev1.BasicAuthPasswordKey,
		),
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   dbApp,
			Labels: podLabels,
		},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				tlsVolume,
				configVolume,
				dataVolume,
			},
			InitContainers: []corev1.Container{
				{
					Name: "init",
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      tlsVolume.Name,
							MountPath: dbTLSDir,
						},
						{
							Name:      configVolume.Name,
							MountPath: dbConfigDir,
						},
						{
							Name:      dataVolume.Name,
							MountPath: dbDataDir,
						},
					},
					Image: dbImage,
					Command: []string{
						"/bin/bash",
						"-c",
						initScript,
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name: "server",
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      configVolume.Name,
							MountPath: dbConfigDir,
						},
						{
							Name:      dataVolume.Name,
							MountPath: dbDataDir,
						},
					},
					Image: dbImage,
					Env:   podEnv,
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: dbPort,
							Protocol:      corev1.ProtocolTCP,
						},
					},
				},
			},
		},
	}
	_, err = s.coreV1.Pods(s.project).Create(pod)
	if errors.IsAlreadyExists(err) {
		err = nil
	}
	if err != nil {
		return err
	}

	// Create the service:
	serviceLabels := map[string]string{
		internal.AppLabel: dbApp,
	}
	serviceAnnotations := map[string]string{
		"service.alpha.openshift.io/serving-cert-secret-name": dbTLSSecretName,
	}
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        dbApp,
			Labels:      serviceLabels,
			Annotations: serviceAnnotations,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				internal.AppLabel: dbApp,
			},
			Ports: []corev1.ServicePort{
				{
					Port:       dbPort,
					TargetPort: intstr.FromInt(dbPort),
				},
			},
		},
	}
	service, err = s.coreV1.Services(s.project).Create(service)
	if errors.IsAlreadyExists(err) {
		err = nil
	}
	if err != nil {
		return err
	}

	// Wait till the pod is ready:
	pod, err = internal.WaitForPod(s.coreV1, s.project, pod.Name)
	if err != nil {
		return err
	}

	// Calculate the database address:
	s.dbAddress = fmt.Sprintf("%s.%s.svc:%d", dbApp, s.project, dbPort)

	// In order to wait for the database to respond we need to create a connection with a short
	// timeout, otherwise it takes very long to respond:
	adminURL := s.dbURL(
		s.dbAdminUser,
		s.dbAdminPassword,
		s.dbAddress,
		dbAdminDatabase,
		map[string]string{
			"connect_timeout": "1",
		},
	)
	err = internal.WaitForDB(adminURL)
	if err != nil {
		return err
	}

	// Create the sequence that will be used to generate unique user and database names:
	adminHandle, err := sql.Open(dbDriver, adminURL.String())
	if err != nil {
		return err
	}
	adminClose := func() {
		err := adminHandle.Close()
		if err != nil {
			log.Errorf("Can't close database administrator handle: %v", err)
		}
	}
	defer adminClose()
	_, err = adminHandle.Exec("CREATE SEQUENCE IF NOT EXISTS sandbox")
	if err != nil {
		return err
	}

	// The database server is now ready:
	s.dbReady = true

	return nil
}

func (s *Sandbox) ensureDBCredentials() error {
	// Generate a random password for the database administrator:
	id, err := uuid.NewRandom()
	if err != nil {
		return err
	}
	s.dbAdminUser = dbAdminUser
	s.dbAdminPassword = id.String()

	// Try to save the generated administrator password to a secret. If this fails because the
	// secret already exists then we discard the password that we generated and use the one in
	// the existing secret instead.
	labels := map[string]string{
		internal.AppLabel: dbApp,
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   dbAdminSecretName,
			Labels: labels,
		},
		Type: corev1.SecretTypeBasicAuth,
		Data: map[string][]byte{
			corev1.BasicAuthUsernameKey: []byte(s.dbAdminUser),
			corev1.BasicAuthPasswordKey: []byte(s.dbAdminPassword),
		},
	}
	secrets := s.coreV1.Secrets(s.project)
	secret, err = secrets.Create(secret)
	if errors.IsAlreadyExists(err) {
		secret, err = secrets.Get(dbAdminSecretName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		var data []byte
		var ok bool
		data, ok = secret.Data[corev1.BasicAuthUsernameKey]
		if !ok {
			return fmt.Errorf(
				"database administator credentials secret '%s' already exists but "+
					"it doesn't contain the '%s' key",
				secret.Name, corev1.BasicAuthUsernameKey,
			)
		}
		if len(data) == 0 {
			return fmt.Errorf(
				"database administrator credentials secret '%s' already exist but "+
					"the '%s' key is empty",
				secret.Name, corev1.BasicAuthUsernameKey,
			)
		}
		s.dbAdminUser = string(data)
		data, ok = secret.Data[corev1.BasicAuthPasswordKey]
		if !ok {
			return fmt.Errorf(
				"database administator credentials secret '%s' already exists but "+
					"it doesn't contain the '%s' key",
				secret.Name, corev1.BasicAuthPasswordKey,
			)
		}
		if len(data) == 0 {
			return fmt.Errorf(
				"database administrator credentials secret '%s' already exist but "+
					"the '%s' key is empty",
				secret.Name, corev1.BasicAuthPasswordKey,
			)
		}
		s.dbAdminPassword = string(data)
		err = nil
	}
	if err != nil {
		return err
	}

	return nil
}

// dbURL makes a database connection URL string from a set connection details.
func (s *Sandbox) dbURL(user, password, address, name string,
	options map[string]string) *url.URL {
	query := url.Values{}
	for name, value := range options {
		query.Set(name, value)
	}
	return &url.URL{
		Scheme:   dbDriver,
		User:     url.UserPassword(user, password),
		Host:     address,
		Path:     name,
		RawQuery: query.Encode(),
	}
}

// Values labels specific to the database:
const (
	dbApp = "database"
)

// Names of secrets specific to the database:
const (
	dbImage           = "centos/postgresql-10-centos7"
	dbTLSSecretName   = "database-tls"
	dbAdminSecretName = "database-admin"
)

// Connection details:
const (
	dbDriver        = "postgres"
	dbAdminDatabase = "postgres"
	dbAdminUser     = "postgres"
	dbPort          = 5432
)

// Directory names:
const (
	dbTLSDir    = "/etc/pki/tls/pgsql"
	dbConfigDir = "/opt/app-root/src/postgresql-cfg"
	dbDataDir   = "/var/lib/pgsql/data"
)

// Template used to generate the script that generates the configuration for the PostgreSQL server:
var dbInitScriptTemplate = `
# Install the TLS certificates:
install \
--mode=0600 \
{{ .TLSDir }}/tls.crt \
{{ .TLSDir }}/tls.key \
{{ .DataDir }}

# Create the TLS configuration:
cat > {{ .ConfigDir }}/tls.conf <<.
ssl = on
ssl_cert_file = '{{ .DataDir }}/tls.crt'
ssl_key_file = '{{ .DataDir }}/tls.key'
.

# Enable the query log:
cat > {{ .ConfigDir }}/log.conf <<.
log_destination = 'stderr'
log_statement = 'all'
logging_collector = off
.
`
