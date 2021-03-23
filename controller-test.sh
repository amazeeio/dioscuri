#!/bin/bash
NOCOLOR='\033[0m'
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
LIGHTBLUE='\033[1;34m'
MAGENTA='\033[1;35m'

DIOSCURI_IMAGE=amazeeio/dioscuri:test-tag
DIOSCURI_NS=dioscuri-controller
CHECK_TIMEOUT=10

KIND_NAME=dioscuri

check_dioscuri_log () {
    echo -e "${GREEN}========= FULL DIOSCURI LOG =========${NOCOLOR}"
    kubectl -n ${DIOSCURI_NS} logs $(kubectl get pods  -n ${DIOSCURI_NS} --no-headers | awk '{print $1}') -c manager
}

tear_down () {
    echo -e "${GREEN}============= TEAR DOWN =============${NOCOLOR}"
    kind delete cluster --name ${KIND_NAME}
}

build_deploy_dioscuri () {
    echo -e "${GREEN}==>${NOCOLOR} Build and deploy dioscuri"
    make docker-build IMG=${DIOSCURI_IMAGE}
    kind load docker-image ${DIOSCURI_IMAGE} --name ${KIND_NAME}
    make deploy IMG=${DIOSCURI_IMAGE}

    CHECK_COUNTER=1
    echo -e "${GREEN}==>${NOCOLOR} Ensure dioscuri is running"
    until $(kubectl get pods -n ${DIOSCURI_NS} --no-headers | grep -q "Running")
    do
        if [ $CHECK_COUNTER -lt $CHECK_TIMEOUT ]; then
            let CHECK_COUNTER=CHECK_COUNTER+1
            echo "dioscuri not running yet"
            sleep 5
        else
            echo "Timeout of $CHECK_TIMEOUT for dioscuri startup reached"
            check_dioscuri_log
            tear_down
            exit 1
        fi
    done
}

echo -e "${GREEN}==>${NOCOLOR} Create dioscuri kind cluster"
kind create cluster --image kindest/node:v1.17.0 --name ${KIND_NAME} --config test-resources/kind-cluster.yaml
kubectl cluster-info --context kind-${KIND_NAME}
kubectl config use-context kind-${KIND_NAME}

NUM_PODS=$(kubectl -n ingress-nginx get pods | grep -ow "Running"| wc -l |  tr  -d " ")
if [ $NUM_PODS -ne 1 ]; then
    echo -e "${GREEN}===>${NOCOLOR} Install ingress-nginx"
    kubectl create namespace ingress-nginx
    helm upgrade --install -n ingress-nginx ingress-nginx ingress-nginx/ingress-nginx -f test-resources/ingress-nginx-values.yaml
    echo -e "${GREEN}===>${NOCOLOR} Wait for ingress-nginx to become ready"
    # sleep 10 #long sleep to give ingress to start up properly
else
    echo -e "${GREEN}===>${NOCOLOR} Ingress-nginx is ready"
fi

CHECK_COUNTER=1
echo -e "${GREEN}==>${NOCOLOR} Ensure ingress-nginx is running"
until $(kubectl get pods -n ingress-nginx --no-headers | grep -q "Running")
do
    if [ $CHECK_COUNTER -lt $CHECK_TIMEOUT ]; then
        let CHECK_COUNTER=CHECK_COUNTER+1
        echo "ingress-nginx not running yet"
        sleep 5
    else
        echo "Timeout of $CHECK_TIMEOUT for ingress-nginx startup reached"
        tear_down
        exit 1
    fi
done

build_deploy_dioscuri

echo -e "${GREEN}==>${NOCOLOR} Build and push nginx test images"
docker build -t amazeeio/nginx-test:ns1 -f test-resources/ns1.Dockerfile test-resources/
docker build -t amazeeio/nginx-test:ns2 -f test-resources/ns2.Dockerfile test-resources/
kind load docker-image amazeeio/nginx-test:ns1 --name ${KIND_NAME}
kind load docker-image amazeeio/nginx-test:ns2 --name ${KIND_NAME}

echo -e "${GREEN}==>${NOCOLOR} Install nginx test apps and associated ingress"
kubectl apply -f test-resources/test-ns1.yaml
kubectl apply -f test-resources/test-ns2.yaml

echo -e "${GREEN}==>${NOCOLOR} Testing ingress is in test-ns1"
CHECK_COUNTER=1
until $(curl -s -I -H "Host: test1.localhost" http://localhost:8090/ | grep -q "200 OK")
do
    if [ $CHECK_COUNTER -lt $CHECK_TIMEOUT ]; then
        let CHECK_COUNTER=CHECK_COUNTER+1
        echo "test1.localhost not running yet"
        sleep 5
    else
        echo "Timeout of $CHECK_TIMEOUT for test1.localhost startup reached"
        tear_down
        exit 1
    fi
done
if curl -s -H "Host: test1.localhost" http://localhost:8090/ | grep -q "test-ns1"; then
    echo -e "${GREEN}===>${NOCOLOR} test-ns1 running"
else
    echo -e "${YELLOW}===>${NOCOLOR} test-ns1 not running"
    curl -s -H "Host: test1.localhost" http://localhost:8090/
    tear_down
    exit 1
fi
echo -e "${GREEN}==>${NOCOLOR} Testing ingress is in test-ns2"
CHECK_COUNTER=1
until $(curl -s -I -H "Host: test1-standby.localhost" http://localhost:8090/ | grep -q "200 OK")
do
    if [ $CHECK_COUNTER -lt $CHECK_TIMEOUT ]; then
        let CHECK_COUNTER=CHECK_COUNTER+1
        echo "test1-standby.localhost not running yet"
        sleep 5
    else
        echo "Timeout of $CHECK_TIMEOUT for test1-standby.localhost startup reached"
        tear_down
        exit 1
    fi
done
if curl -s -H "Host: test1-standby.localhost" http://localhost:8090/ | grep -q "test-ns2"; then
    echo -e "${GREEN}===>${NOCOLOR} test-ns2 running"
else
    echo -e "${YELLOW}===>${NOCOLOR} test-ns2 not running"
    curl -s -H "Host: test1-standby.localhost" http://localhost:8090/
    tear_down
    exit 1
fi

echo -e "${GREEN}==>${NOCOLOR} Get ingresses"
kubectl get ingress --all-namespaces


echo -e "${GREEN}==>${NOCOLOR} Run the migration"
kubectl apply -f test-resources/hostmigration-ns1.yaml
sleep 5

MIGRATION_CHECK_TIMEOUT=20
CHECK_COUNTER=1
until $(kubectl get hostmigration -n test-ns1 active-standby -o json | jq -r '.status.conditions[].type' | grep -q "completed")
do
    if kubectl get hostmigration -n test-ns1 active-standby -o json | jq -r '.status.conditions[].type' | grep -q "failed"; then
        kubectl get hostmigration -n test-ns1 active-standby -o yaml
        tear_down
        exit 1
    fi
    if [ $CHECK_COUNTER -lt $MIGRATION_CHECK_TIMEOUT ]; then
        let CHECK_COUNTER=CHECK_COUNTER+1
        echo "hostmigration not complete yet"
        sleep 5
    else
        echo "Timeout of $MIGRATION_CHECK_TIMEOUT for hostmigration completion reached"
        kubectl get hostmigration -n test-ns1 active-standby -o yaml
        tear_down
        exit 1
    fi
done

echo -e "${GREEN}==>${NOCOLOR} Get ingresses"
kubectl get ingress --all-namespaces

echo -e "${GREEN}==>${NOCOLOR} Testing ingress moved to test-ns2"
CHECK_COUNTER=1
until $(curl -s -I -H "Host: test1.localhost" http://localhost:8090/ | grep -q "200 OK")
do
    if [ $CHECK_COUNTER -lt $CHECK_TIMEOUT ]; then
        let CHECK_COUNTER=CHECK_COUNTER+1
        echo "test1.localhost not running yet"
        sleep 5
    else
        echo "Timeout of $CHECK_TIMEOUT for test1.localhost startup reached"
        tear_down
        exit 1
    fi
done
if curl -s -H "Host: test1.localhost" http://localhost:8090/ | grep -q "test-ns2"; then
    echo -e "${GREEN}===>${NOCOLOR} test-ns2 running"
else
    echo -e "${YELLOW}===>${NOCOLOR} test-ns2 not running"
    curl -s -H "Host: test1.localhost" http://localhost:8090/
    tear_down
    exit 1
fi
echo -e "${GREEN}==>${NOCOLOR} Testing standby ingress moved to test-ns1"
CHECK_COUNTER=1
until $(curl -s -I -H "Host: test1-standby.localhost" http://localhost:8090/ | grep -q "200 OK")
do
    if [ $CHECK_COUNTER -lt $CHECK_TIMEOUT ]; then
        let CHECK_COUNTER=CHECK_COUNTER+1
        echo "test1-standby.localhost not running yet"
        sleep 5
    else
        echo "Timeout of $CHECK_TIMEOUT for test1-standby.localhost startup reached"
        tear_down
        exit 1
    fi
done
if curl -s -H "Host: test1-standby.localhost" http://localhost:8090/ | grep -q "test-ns1"; then
    echo -e "${GREEN}===>${NOCOLOR} test-ns1 running"
else
    echo -e "${YELLOW}===>${NOCOLOR} test-ns1 not running"
    curl -s -H "Host: test1-standby.localhost" http://localhost:8090/
    tear_down
    exit 1
fi

echo -e "${GREEN}==>${NOCOLOR} Run the migration again to make sure they go back"
kubectl apply -f test-resources/hostmigration-ns2.yaml
sleep 5

MIGRATION_CHECK_TIMEOUT=20
CHECK_COUNTER=1
until $(kubectl get hostmigration -n test-ns2 active-standby -o json | jq -r '.status.conditions[].type' | grep -q "completed")
do
    if kubectl get hostmigration -n test-ns2 active-standby -o json | jq -r '.status.conditions[].type' | grep -q "failed"; then
        kubectl get hostmigration -n test-ns2 active-standby -o yaml
        tear_down
        exit 1
    fi
    if [ $CHECK_COUNTER -lt $MIGRATION_CHECK_TIMEOUT ]; then
        let CHECK_COUNTER=CHECK_COUNTER+1
        echo "hostmigration not complete yet"
        sleep 5
    else
        echo "Timeout of $MIGRATION_CHECK_TIMEOUT for hostmigration completion reached"
        kubectl get hostmigration -n test-ns1 active-standby -o yaml
        tear_down
        exit 1
    fi
done

echo -e "${GREEN}==>${NOCOLOR} Get ingresses"
kubectl get ingress --all-namespaces

echo -e "${GREEN}==>${NOCOLOR} Testing ingress is in test-ns1"
CHECK_COUNTER=1
until $(curl -s -I -H "Host: test1.localhost" http://localhost:8090/ | grep -q "200 OK")
do
    if [ $CHECK_COUNTER -lt $CHECK_TIMEOUT ]; then
        let CHECK_COUNTER=CHECK_COUNTER+1
        echo "test1.localhost not running yet"
        sleep 5
    else
        echo "Timeout of $CHECK_TIMEOUT for test1.localhost startup reached"
        tear_down
        exit 1
    fi
done
if curl -s -H "Host: test1.localhost" http://localhost:8090/ | grep -q "test-ns1"; then
    echo -e "${GREEN}===>${NOCOLOR} test-ns1 running"
else
    echo -e "${YELLOW}===>${NOCOLOR} test-ns1 not running"
    curl -s -H "Host: test1.localhost" http://localhost:8090/
    tear_down
    exit 1
fi
echo -e "${GREEN}==>${NOCOLOR} Testing ingress is in test-ns2"
CHECK_COUNTER=1
until $(curl -s -I -H "Host: test1-standby.localhost" http://localhost:8090/ | grep -q "200 OK")
do
    if [ $CHECK_COUNTER -lt $CHECK_TIMEOUT ]; then
        let CHECK_COUNTER=CHECK_COUNTER+1
        echo "test1-standby.localhost not running yet"
        sleep 5
    else
        echo "Timeout of $CHECK_TIMEOUT for test1-standby.localhost startup reached"
        tear_down
        exit 1
    fi
done
if curl -s -H "Host: test1-standby.localhost" http://localhost:8090/ | grep -q "test-ns2"; then
    echo -e "${GREEN}===>${NOCOLOR} test-ns2 running"
else
    echo -e "${YELLOW}===>${NOCOLOR} test-ns2 not running"
    curl -s -H "Host: test1-standby.localhost" http://localhost:8090/
    tear_down
    exit 1
fi

echo -e "${GREEN}==>${NOCOLOR} Clean up"
tear_down