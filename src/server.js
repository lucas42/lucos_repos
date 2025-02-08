import express from 'express';
import { execSync } from 'child_process';

const port = process.env.PORT;
if (!port) throw "no PORT environment variable set";
const git_email = process.env.GIT_EMAIL;
if (!git_email) throw "no GIT_EMAIL environment variable set";


execSync(
	"git config --global user.name 'lucos_repos'",
	{stdio: 'inherit'},
);
execSync(
	`git config --global user.email "${git_email}"`,
	{stdio: 'inherit'},
);

const app = express();

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

app.listen(port, () => {
  console.log(`Server Listening on port ${port}`)
});

// Wrapper for controllor async functions which catches errors and sends them on to express' error handling
function catchErrors(controllerFunc) {
	return ((req, res, next) => {
		controllerFunc(req, res).catch(error => next(error));
	});
}