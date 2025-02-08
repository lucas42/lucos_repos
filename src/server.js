import express from 'express';
import { chdir } from 'process';
import { writeFile, mkdir } from 'fs/promises';
import { exec as raw_exec, execFile as raw_execFile, execSync } from 'child_process';
import { promisify } from 'util';
const exec = promisify(raw_exec);
const execFile = promisify(raw_execFile);

const port = process.env.PORT;
if (!port) throw "no PORT environment variable set";
const git_email = process.env.GIT_EMAIL;
if (!git_email) throw "no GIT_EMAIL environment variable set";
const github_ssh = process.env.GITHUB_SSH;
if (!github_ssh) throw "no GITHUB_SSH environment variable set";


execSync(
	"git config --global user.name 'lucos_repos'",
	{stdio: 'inherit'},
);
execSync(
	`git config --global user.email "${git_email}"`,
	{stdio: 'inherit'},
);
await mkdir('/root/.ssh', { recursive: true })
await writeFile('/root/.ssh/id_ed25519', process.env.GITHUB_SSH, {mode:'600'});

// Check that SSH is set up correctly and add github.com to trusted hosts
try {
	await execSync("ssh -T git@github.com -o StrictHostKeyChecking=accept-new", {stdio: 'inherit'});
} catch (error) {
	// Expected exit code is 1 - throw the error for anything else
	if (error.status != 1) throw error;
}

const app = express();
app.use(express.json());

app.get('/_info', catchErrors(async (req, res) => {
	res.json({
		system: 'lucos_repos',
		checks: {},
		metrics: {},
		ci: {
			circle: "gh/lucas42/lucos_repos",
		},
		network_only: true,
		show_on_homepage: false,
	});
}));

app.post("/github/webhook", catchErrors(async (req, res) => {
	const eventType = req.get("X-GitHub-Event");
	const signature = req.get("X-Hub-Signature-256"); // TODO: verify signature if any sensitive logic is added below
	if (eventType == "push") {
		const ref = req.body.ref;
		const repo_owner = req.body.repository.owner.login;
		if (repo_owner != "lucas42") throw `${repo_owner} doesn't even go here`;
		const repo_name = req.body.repository.name;
		const head_committer = req.body.head_commit.committer.email;
		const ssh_url = req.body.repository.ssh_url;
		console.log(`INFO Github webhook called eventType=${eventType}, ref=${ref}, repo_name=${repo_name}, head_committer=${head_committer}`);
		if (ref != "refs/heads/main") {
			console.log("DEBUG Ignore - not on main branch");
			return res.send("No Action: Not on main");
		}
		if (head_committer == git_email) {
			console.log("DEBUG Ignore - push caused by this service");
			return res.send("No Action: Push came from this service");
		}
		try {
			chdir(`/usr/src/repos/${repo_name}`);
		} catch {
			console.log(`DEBUG Local copy of ${repo_name} not found - cloning...`)
			chdir("/usr/src/repos");
			await execFile('git', ['clone', ssh_url], {stdio: 'inherit'});
			chdir(`/usr/src/repos/${repo_name}`);
		}
		await exec("git pull --rebase --autostash", {stdio: 'inherit'});
		await exec("npm version patch", {stdio: 'inherit'});
		await exec("git push --follow-tags origin", {stdio: 'inherit'});
		res.send("New version pushed");
	} else {
		console.error(`ERROR Unknown webhook from github of type ${eventType}`);
		throw `Unknown event type ${eventType}`;
	}
}));

app.listen(port, () => {
  console.log(`INFO Server Listening on port ${port}`)
});

// Wrapper for controllor async functions which catches errors and sends them on to express' error handling
function catchErrors(controllerFunc) {
	return ((req, res, next) => {
		controllerFunc(req, res).catch(error => next(error));
	});
}