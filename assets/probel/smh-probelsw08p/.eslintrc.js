module.exports = {
    env: {
        es6: true,
        node: true,
        jest: true
    },
    parser: '@typescript-eslint/parser',
    parserOptions: {
        project: 'tsconfig.json',
        sourceType: 'module'
    },
    plugins: ['@typescript-eslint', '@typescript-eslint/tslint', 'simple-import-sort', 'unused-imports', 'prettier'],
    ignorePatterns: ['test/**', 'src/model/generated/**'],
    rules: {
        'prettier/prettier': 'error',
        'unused-imports/no-unused-imports-ts': 'error',
        '@typescript-eslint/class-name-casing': 'error',
        '@typescript-eslint/consistent-type-definitions': 'error',
        '@typescript-eslint/explicit-member-accessibility': [
            'off',
            {
                accessibility: 'explicit'
            }
        ],
        '@typescript-eslint/member-delimiter-style': [
            'error',
            {
                multiline: {
                    delimiter: 'semi',
                    requireLast: true
                },
                singleline: {
                    delimiter: 'semi',
                    requireLast: false
                }
            }
        ],
        '@typescript-eslint/member-ordering': 'error',
        '@typescript-eslint/no-empty-function': 'off',
        '@typescript-eslint/no-empty-interface': 'error',
        '@typescript-eslint/no-misused-new': 'error',
        '@typescript-eslint/no-non-null-assertion': 'error',
        '@typescript-eslint/no-use-before-define': 'off',
        '@typescript-eslint/prefer-function-type': 'error',
        '@typescript-eslint/semi': ['error', 'always'],
        '@typescript-eslint/type-annotation-spacing': 'error',
        '@typescript-eslint/no-inferrable-types': [
            'error',
            {
                ignoreParameters: true
            }
        ],
        '@typescript-eslint/explicit-function-return-type': [
            'error',
            {
                allowTypedFunctionExpressions: true
            }
        ],
        '@typescript-eslint/unified-signatures': 'error',
        'arrow-body-style': 'error',
        camelcase: 'off',
        'comma-dangle': 'error',
        'constructor-super': 'error',
        curly: 'error',
        'dot-notation': 'off',
        'eol-last': 'error',
        eqeqeq: ['error', 'smart'],
        'guard-for-in': 'error',
        'id-blacklist': 'off',
        'id-match': 'off',
        'no-restricted-imports': [
            'error',
            {
                paths: ['rxjs']
            }
        ],
        'simple-import-sort/sort': 'error',
        'no-bitwise': 'off',
        'no-caller': 'error',
        'no-console': [
            'error',
            {
                allow: [
                    'log',
                    'dirxml',
                    'warn',
                    'error',
                    'dir',
                    'timeLog',
                    'assert',
                    'clear',
                    'count',
                    'countReset',
                    'group',
                    'groupCollapsed',
                    'groupEnd',
                    'table',
                    'Console',
                    'markTimeline',
                    'profile',
                    'profileEnd',
                    'timeline',
                    'timelineEnd',
                    'timeStamp',
                    'context'
                ]
            }
        ],
        'no-debugger': 'error',
        'no-empty': 'off',
        'no-eval': 'error',
        'no-fallthrough': 'error',
        'no-multiple-empty-lines': [
            'error',
            {
                max: 1
            }
        ],
        'no-new-wrappers': 'error',
        'no-redeclare': 'error',
        'no-shadow': [
            'error',
            {
                hoist: 'all'
            }
        ],
        'no-throw-literal': 'error',
        'no-trailing-spaces': 'error',
        'no-undef-init': 'error',
        'no-underscore-dangle': 'off',
        'no-unused-expressions': 'error',
        'no-unused-labels': 'error',
        'no-var': 'error',
        'prefer-const': 'error',
        radix: 'error',
        'spaced-comment': 'error',
        'valid-typeof': 'off',
        'object-curly-spacing': ['error', 'always'],
        '@typescript-eslint/tslint/config': [
            'error',
            {
                rules: {
                    'one-line': [true, 'check-open-brace', 'check-catch', 'check-else', 'check-whitespace'],
                    whitespace: [
                        true,
                        'check-branch',
                        'check-decl',
                        'check-operator',
                        'check-separator',
                        'check-type',
                        'check-module'
                    ]
                }
            }
        ]
    }
};
