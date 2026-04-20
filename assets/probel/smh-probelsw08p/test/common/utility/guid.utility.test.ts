import 'reflect-metadata';

import { GuidUtility } from '../../../src/common/utility/guid.utility';

// Used to access the protected ctor of GuidHelper and throw an error
class GuidHelperEx extends GuidUtility {
    constructor() {
        super();
    }
}

describe('GuidUtility', () => {
    it('should throw an error when ctor is called', () => {
        // Arrange

        // Act
        const util = (): GuidHelperEx => new GuidHelperEx();

        // Assert
        expect(util).toThrowError();
    });

    it('should generate GUID', () => {
        // E.g. - 2baa909c-1745-11ea-8d71-362b9e155667
        // Arrange
        const uuidV4Regex = /^[A-F\d]{8}-[A-F\d]{4}-4[A-F\d]{3}-[89AB][A-F\d]{3}-[A-F\d]{12}$/i;

        // Act
        const uuid = GuidUtility.generateUuid();

        // Assert
        expect(uuid).toBeDefined();
        expect(uuid.length).toBe(36);
        expect(uuidV4Regex.test(uuid)).toBe(true);
    });
});
