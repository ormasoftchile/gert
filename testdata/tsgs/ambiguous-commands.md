# Emergency Cleanup

## Remove Stale Resources

Delete old deployments that are no longer needed:

```bash
rm -rf /var/data/old-deployments/
```

## Restart Services

Restart all services to apply the new configuration:

```bash
sudo systemctl restart critical-service
```

## Purge Credentials Cache

Clear the credentials cache to force re-authentication:

```bash
rm -f ~/.aws/credentials
```
