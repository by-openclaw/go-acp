import 'reflect-metadata';

import { Maybe } from '../../../src/common/util/type';
import { TypeUtility } from '../../../src/common/utility/type.utility';

class TypeHelperEx extends TypeUtility {
    constructor() {
        super();
    }
}

describe('TypeUtility', () => {
    it('should throw an error when ctor is called', () => {
        // Arrange

        // Act
        const util = (): TypeHelperEx => new TypeHelperEx();

        // Assert
        expect(util).toThrowError();
    });

    it('should get value or undefined (no null returned)', () => {
        // Arrange
        const stringVar: Maybe<string> = 'test';
        const nullVar: Maybe<string> = null;
        const undefinedVar: Maybe<string> = undefined;

        // Act
        const targetUndefinedVar = TypeUtility.getValueOrUndefined(undefinedVar);
        const targetStringVar = TypeUtility.getValueOrUndefined(stringVar);
        const targetNullVar = TypeUtility.getValueOrUndefined(nullVar);

        // Assert
        expect(targetUndefinedVar).toBeUndefined();
        expect(targetStringVar).toBe(stringVar);
        expect(targetNullVar).toBeUndefined();
    });
});
