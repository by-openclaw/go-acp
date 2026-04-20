module.exports = {
    inputFiles: ['./src/'],
    //mode: 'file',
    mode: 'modules',
    includeDeclarations: true,
    tsconfig: 'tsconfig.json',
    out: './docs/dist/docs-class-diagram-modules/',
    excludePrivate: false,
    excludeProtected: false,
    excludeExternals: true,
    readme: './README.md',
    name: 'Probel SW-P-8 protocol Library project',
    ignoreCompilerErrors: true,
    plugin: 'typedoc-umlclass',
    listInvalidSymbolLinks: true,
    entryPoint: 'index',
    categorizeByGroup: true,
    media: './docs/dist/docs-class-diagram/assets',
    version: '',
    gitRemote: 'remote',
    gitRevision: 'revision',
    theme: 'default',

    // Basic Settings
    umlClassDiagramType: 'detailed',
    umlClassDiagramLocation: 'embed',
    //umlClassDiagramFormat: 'png',
    umlClassDiagramFormat: 'svg',

    // HTML output
    umlClassDiagramSectionTitle: 'Class-Diagram',
    umlClassDiagramPosition: 'above',

    // Class diagram formating
    umlClassDiagramMethodParameterOutput: 'complete',
    umlClassDiagramHideEmptyMembers: 'false',
    // umlClassDiagramTopDownLayoutMaxSiblings: 6,
    umlClassDiagramMemberVisibilityStyle: 'icon',
    umlClassDiagramHideCircledChar: 'false',
    umlClassDiagramHideShadow: 'false',
    //umlClassDiagramBoxBackgroundColor: transparent | #RGBHEX ,
    //umlClassDiagramBoxBorderColor: transparent | #RGBHEX ,
    umlClassDiagramBoxBorderRadius: 2,
    //umlClassDiagramBoxBorderWidth: 1,
    //umlClassDiagramArrowColor: #RGBHEX ,
    umlClassDiagramClassFontName: 'Courier',
    //umlClassDiagramClassFontSize: integer ,
    umlClassDiagramClassFontStyle: 'bold',
    //umlClassDiagramClassFontColor: transparent|#RGBHEX ,
    //umlClassDiagramClassAttributeFontName: font-name ,
    umlClassDiagramClassAttributeFontStyle: 'italic',
    //umlClassDiagramClassAttributeFontColor: transparent|#RGBHEX,

    // Other settings
    umlClassDiagramHideProgressBar: true,
    umlClassDiagramCreatePlantUmlFiles: true,
    umlClassDiagramVerboseOutput: false
};
