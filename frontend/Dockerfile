FROM node:24-alpine AS dependencies
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

FROM dependencies AS assets
COPY download-fonts.mjs ./
RUN node download-fonts.mjs

FROM assets AS development
COPY . .
EXPOSE 8080
CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0", "--port", "8080", "--strictPort"]

FROM development AS build
RUN npm run build

FROM nginx:1.28-alpine AS production
COPY nginx.conf /etc/nginx/nginx.conf
COPY --from=build /app/dist /usr/share/nginx/html
USER nginx
EXPOSE 8080
ENTRYPOINT ["nginx"]
CMD ["-g", "daemon off;"]

