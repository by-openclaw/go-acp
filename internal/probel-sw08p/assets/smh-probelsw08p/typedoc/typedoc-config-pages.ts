module.exports = {
    inputFiles: ['./source/'],
    tsconfig: 'tsconfig.json',
    listInvalidSymbolLinks: false,
    media: './static',
    name: 'Probel SW-P-8 protocol Library project',
    out: './docs/dist/docs-pages/',
    readme: './README.md',
    theme: 'pages-plugin',
    // mode: 'file',
    // categorizeByGroup: true,
    // includeDeclarations: true,
    // excludePrivate: false,
    // excludeProtected: false,
    // excludeExternals: true,
    includeVersion: true,
    // ignoreCompilerErrors: true,
    // plugin: 'typedoc-plugin-pages'
    // plugin: 'typedoc-plantuml',
    // umlLocation: 'local',
    // umlFormat: 'svg'
    pages: {
        groups: [
            {
                title: 'Getting Started',
                source: './getting-started',
                pages: [
                    {
                        title: 'Quick Start',
                        source: './quick-start.md'
                    }
                ]
            },
            {
                output: 'configuration',
                title: 'Configurations',
                pages: [
                    {
                        title: 'Network',
                        source: './configuration/configuration-file.md'
                    },
                    {
                        title: 'Settings',
                        source: './configuration/configuration-file.md'
                    },
                    {
                        title: 'Loggin',
                        source: './configuration/configuration-file.md'
                    }
                ]
            },
            {
                title: 'Advanced Content',
                source: './advanced-content',
                pages: [
                    {
                        title: 'Images',
                        source: './images.md'
                    },
                    {
                        title: 'Code Links',
                        source: './code-links.md'
                    },
                    {
                        title: 'Page Links',
                        source: './page-links.md',
                        output: 'page-links.html'
                    }
                ]
            },
            {
                title: 'Class Diagram',
                source: './advanced-content',
                pages: [
                    {
                        title: 'Images',
                        source: './images.md'
                    },
                    {
                        title: 'Code Links',
                        source: './code-links.md'
                    },
                    {
                        title: 'Page Links',
                        source: './page-links.md',
                        output: 'page-links.html'
                    }
                ]
            },
            {
                title: 'CLI',
                source: './advanced-content',
                pages: [
                    {
                        title: 'Provider',
                        source: './images.md'
                    },
                    {
                        title: 'Consumer',
                        source: './code-links.md'
                    }
                ]
            },
            {
                title: 'References',
                source: './advanced-content',
                pages: [
                    {
                        title: 'Provider',
                        source: './images.md'
                    },
                    {
                        title: 'Consumer',
                        source: './code-links.md'
                    }
                ]
            },
            ,
            {
                title: 'IDE',
                source: './advanced-content',
                pages: [
                    {
                        title: 'VSCode',
                        source: './images.md'
                    }
                ]
            }
            {
                title: 'License',
                source: './advanced-content',
                pages: [
                    {
                        title: 'Copyright',
                        source: './images.md'
                    },
                    {
                        title: 'Third Parties',
                        source: './code-links.md'
                    }
                ]
            },
            {
                title: 'API',
                source: './advanced-content',
                pages: [
                    {
                        title: 'RestApi + Websocket',
                        source: './images.md'
                    },
                    {
                        title: 'RestApi + GraphQL',
                        source: './code-links.md'
                    }
                ]
            }
        ],
        output: 'pages',
        reflectionNavigationTitle: 'Library',
        replaceGlobalsPage: true,
        source: './docs-source'
    }
};
