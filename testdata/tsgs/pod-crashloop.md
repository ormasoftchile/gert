# Pod CrashLoopBackOff Investigation

## 1. Check Pod Status

Run the following to see pod status in the affected namespace:

```bash
kubectl get pods -n $NAMESPACE -l app=$SERVICE
```

Look for pods in CrashLoopBackOff or Error state.

## 2. Get Pod Logs

Retrieve logs from the crashing pods:

```bash
kubectl logs -n $NAMESPACE -l app=$SERVICE --tail=100
```

## 3. Check Events

Review recent cluster events:

```bash
kubectl get events -n $NAMESPACE --sort-by=.lastTimestamp
```

## 4. Validate Monitoring Dashboard

Open the Grafana dashboard for the service and confirm:
- Error rate is below 1%
- P99 latency is below 500ms
- No anomalous traffic patterns

Take a screenshot of the dashboard for evidence.
