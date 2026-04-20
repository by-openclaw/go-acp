import 'reflect-metadata';

import { Maybe } from '../../../src/common/util/type';

describe('Type', () => {
    it('should returns a value | null | undefined', () => {
        // Arrange

        // Act
        const stringVar: Maybe<string> = 'test';
        const nullVar: Maybe<string> = null;
        const undefinedVar: Maybe<string> = undefined;

        // Assert
        expect(stringVar).toBe('test');
        expect(nullVar).toBeNull();
        expect(undefinedVar).toBeUndefined();
    });
});
