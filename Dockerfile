FROM node:24.8.0-alpine3.21

WORKDIR /usr/src/app
RUN mkdir /usr/src/repos
RUN apk add git openssh

COPY package* ./
RUN npm install

COPY src .

RUN npm prune --omit=dev

ENV NODE_ENV production
EXPOSE $PORT

CMD [ "npm", "start" ]