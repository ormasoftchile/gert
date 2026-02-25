# Node.js Install Check

## Background

This is a test TSG used to validate the gert runbook engine features — branching, conditions, captures, manual steps, CLI steps, and outcomes.

It checks whether Node.js is installed on the current machine and toggles its state: if installed, it guides through uninstallation; if not installed, it guides through installation.

## Triage

### Step 1: Check if Node.js is installed

Run the following command to check if Node.js is available:

```bash
node --version
```

If the command succeeds, it returns a version string like `v20.11.0`. If it fails, Node.js is not installed.

## Mitigation — Uninstall Path

If Node.js IS installed and the intent is to uninstall:

### Step 2a: Review current version

Note the installed version for reference before proceeding.

### Step 3a: Uninstall Node.js

Uninstall using the appropriate method for your OS:

**Windows:**
```
winget uninstall OpenJS.NodeJS
```

**macOS:**
```
brew uninstall node
```

**Linux:**
```
sudo apt remove nodejs
```

### Step 4a: Verify removal

```bash
node --version
```

If this command fails (command not found), the uninstall was successful.

## Mitigation — Install Path

If Node.js is NOT installed and the intent is to install:

### Step 2b: Confirm installation needed

Node.js was not found on the system. Proceed to install.

### Step 3b: Install Node.js

Install using the appropriate method for your OS:

**Windows:**
```
winget install OpenJS.NodeJS.LTS
```

**macOS:**
```
brew install node
```

**Linux:**
```
sudo apt install nodejs npm
```

### Step 4b: Verify installation

```bash
node --version
```

If this command succeeds and returns a version, the install was successful.

## Escalation

If installation or uninstallation fails after following the steps above, consult the [Node.js documentation](https://nodejs.org) or your system administrator.
