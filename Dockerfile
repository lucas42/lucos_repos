FROM node:23-alpine

WORKDIR /usr/src/app
COPY package* ./

RUN npm install

COPY src .

RUN npm prune --omit=dev

ENV NODE_ENV production
EXPOSE $PORT

CMD [ "npm", "start" ]