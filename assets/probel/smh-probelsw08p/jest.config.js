module.exports = {
    verbose: true,
    transform: {
        '^.+\\.tsx?$': 'ts-jest'
    },
    testEnvironment: 'node',
    testRegex: '(/test/.*|(\\.|/)(test|spec))(\\.it)?\\.(jsx?|tsx?)$',
    testPathIgnorePatterns: ['/coverage', '/lib', '/test/fixture'],
    testResultsProcessor: './node_modules/jest-junit-reporter',
    moduleFileExtensions: ['ts', 'tsx', 'js', 'jsx', 'json', 'node'],
    coverageThreshold: {
        global: {
            branches: 15,
            functions: 15,
            lines: 15,
            statements: 0
        }
    },
    collectCoverageFrom: ['src/**/*.ts', '!src/index.ts'],
    coverageReporters: ['json-summary', 'text', 'lcov'],
    reporters: [
        'default',
        [
            './node_modules/jest-html-reporter',
            {
                pageTitle: 'Test Report - Probel SW-P-08 Library',
                includeFailureMsg: true,
                statusIgnoreFilter: 'passed'
            }
        ]
    ]
};