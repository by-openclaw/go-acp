# SMH NodeJs Typescript Connector Library Template

[![Commitizen friendly](https://img.shields.io/badge/commitizen-friendly-brightgreen.svg)](http://commitizen.github.io/cz-cli/)

![Coverage:branches](./badges/badge-branches.svg)
![Coverage:functions](./badges/badge-functions.svg)
![Coverage:lines](./badges/badge-lines.svg)
![Coverage:statements](./badges/badge-statements.svg)

This `Smart Media Hub` (SMH) `NodeJs Typescript Library` is the `Connector Library Template` that will be used as starter kit for any `NodeJs Typescript Connector Library` developed in the SMH project.

1. Compile

    ```bash
    npm run compile
    ```

2. ESLint

    ```bash
    npm run eslint
    npm run eslint:fix
    ```

3. Test

    Install the vs-code plug-in `Jest Runner`, right-click to show the contextual menu and select on of the 2 options : `Debug Jest`, `Run Jst` or `Run Jest File`.

    ```bash
    npm run test
    ```

4. Code Coverage + Badges generation

    ```bash
    npm run test:coverage
    ```

5. Commit and Push

    Use the script `npm run commit` to commit your code and follow what the CLI ask.

    This will use iteratively `commitizen` and `cz-conventional-changelog`, to enforce us to follow some commit conventions adopted by Angular development. Visit this [CONTRIBUTING](https://github.com/angular/angular/blob/master/CONTRIBUTING.md) link for more info.

    ```bash
    // stage your changes

    npm run commit
    // Follow what the CI proposes

    git push
    ```

6. Generate documentation

    Install the `vs-code` plug-ins `Add jsdoc comments` and `Document This`, right-click the class, function,... and select the `Document This` option. Complete the snippet code provides by the plug-in. For more documentation, follow this link [@use JSDoc](https://jsdoc.app/)

    Install `jsdoc` globally

    To generate the complete `JSDoc` of the project, execute the following script

    ```bash
    npm i -g jsdoc
    ```

    Generate doc by starting the docs script

    ```bash
    npm run docs
    ```

7. Generate documentation for bitbucket, web and plantuml

    Compile, lint, test, with coverage and badges generation.

    ```bash
    npm install --save-dev typedoc typedoc-plugin-markdown typedoc-plugin-pages typedoc-plantuml typedoc-henanigans-theme typedoc-default-themes eledoc typedoc-dark-theme
    ```

    Add this script in package.json

    ```json
        "docs:web": " rimraf ./docs/dist/docs-web && npx typedoc --options ./docs/settings/typedoc-config-web.ts",
        "docs:plantuml": " rimraf ./docs/dist/docs-plantuml && npx typedoc --options ./docs/settings/typedoc-config-plantuml.ts",
        "docs:bitbucket": " rimraf ./docs/dist/bitbucket && npx typedoc --options ./docs/settings/typedoc-config-bitbucket.ts",
        "docs:class:diagram": " rimraf ./docs/dist/class-diagram && typedoc --options ./docs/settings/typedoc-config-class-diagram.ts"
    ```

    To generate markdown docs

    ```bash
    npm run docs:web // default website
    npm run docs:plantuml // default website with plantuml rendering
    npm run docs:bitbucket // bitbucket markdown files with TOC and navigation
    npm run docs:class:diagram // default website with class diagram rendering // doesn't work package issue
    ```

    Missing script to copy "badges" folder into the ./docs/dist/docs-xxxxxxx then we will get the code covarge % value available.

8. Continuous Integration

    Compile, lint, test, with coverage and badges generation.

    ```bash
    npm run ci
    ```

## NodeJs documentation

[Official NodeJs Net API documentation](https://nodejs.org/api/net.html) - asynchronous network API for creating stream-based TCP or IPC servers (net.createServer()) and clients (net.createConnection()).

https://www.geeksforgeeks.org/tcp-connection-termination/

## Remarks

When moving code from `./common/**` to `smh-common` libraty remove the following package from `dependencies` and `devdependencies`

In the mean time, try to test and use a new library for `circular-json` that is obsolete

```bash
npm i circular-json
npm i -D @types/circular-json
```
