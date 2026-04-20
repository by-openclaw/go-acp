import { NotImplementedError } from '../../../src/common/error/not-implemented.error';

describe('NotImplementedError', () => {
    it('should instantiate class without message.', () => {
        // Arrange

        // Act
        const error = new NotImplementedError();

        // Assert
        expect(error).not.toBeNull();
        expect(error.name).toBe(NotImplementedError.name);
        expect(error).toBeInstanceOf(NotImplementedError);
    });

    it('should instantiate class with message.', () => {
        // Arrange
        const message = 'sample error message';

        // Act
        const error = new NotImplementedError(message);

        // Assert
        expect(error).not.toBeNull();
        expect(error.name).toBe(NotImplementedError.name);
        expect(error).toBeInstanceOf(NotImplementedError);
        expect(error.message).toBe(message);
    });
});
