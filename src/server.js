import express from 'express';

const port = process.env.PORT;
if (!port) throw "no PORT environment variable set";

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