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
	res.status(410);
	res.send('This webhook has been replaced by the `calc-version` command in lucos_deploy_orb\n');
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