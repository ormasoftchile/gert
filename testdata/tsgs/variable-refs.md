# Service Health Check

## 1. Check Endpoint Health

Verify the API endpoint is responding:

```bash
curl -s https://$ENDPOINT/health | jq .status
```

## 2. Check Pod Replicas

Verify the expected number of replicas are running:

```bash
kubectl get deployment $SERVICE -n $NAMESPACE -o jsonpath='{.status.readyReplicas}'
```

## 3. Check Resource Usage

Review CPU and memory usage:

```bash
kubectl top pods -n $NAMESPACE -l app=$SERVICE
```

Expected output should show CPU < 80% and memory < 70%.
