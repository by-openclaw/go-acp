import { LocaleDataError } from '../../../src/common/locale-data/locale-data.error';

describe('LocaleDataError', () => {
    it('should instantiate class with message.', () => {
        // Arrange
        const message = 'sample error message';

        // Act
        const error = new LocaleDataError(message);

        // Assert
        expect(error).not.toBeNull();
        expect(error.name).toBe(LocaleDataError.name);
        expect(error).toBeInstanceOf(LocaleDataError);
        expect(error.message).toBe(message);
        expect(error.innerError).toBeUndefined();
    });

    it('should instantiate class with message and inner error.', () => {
        // Arrange
        const message = 'sample error message';
        const innerError = new Error('innerErrorMessage');

        // Act
        const error = new LocaleDataError(message, innerError);

        // Assert
        expect(error).not.toBeNull();
        expect(error.name).toBe(LocaleDataError.name);
        expect(error).toBeInstanceOf(LocaleDataError);
        expect(error.message).toBe(message);
        expect(error.innerError).toBe(innerError);
    });
});
