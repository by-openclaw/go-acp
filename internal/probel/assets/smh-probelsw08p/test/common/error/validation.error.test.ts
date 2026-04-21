import { ValidationError } from '../../../src/common/error/validation.error';
import { LocaleData } from '../../../src/common/locale-data/locale-data.model';

describe('ValidationError', () => {
    it('should instantiate class with message and inner errors.', () => {
        // Arrange
        const message = 'sample error message';
        const innersErrors: Record<string, LocaleData> = {};
        innersErrors.sample1 = { id: '99', description: 'sample description' };
        innersErrors.sample2 = { id: '99', description: 'sample description' };

        // Act
        const error = new ValidationError(message, innersErrors);

        // Assert
        expect(error).not.toBeNull();
        expect(error).toBeInstanceOf(ValidationError);
        expect(error.name).toBe(ValidationError.name);
        expect(error.message).toBe(message);
        expect(error.errors.sample1.id).toBe(innersErrors.sample1.id);
        expect(error.errors.sample2.id).toBe(innersErrors.sample2.id);
    });
});
