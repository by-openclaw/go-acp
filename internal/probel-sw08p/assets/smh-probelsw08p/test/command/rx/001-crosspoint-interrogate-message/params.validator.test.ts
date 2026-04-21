import { CrossPointInterrogateMessageCommandParams } from '../../../../src/command/rx/001-crosspoint-interrogate-message/params';
import { CommandParamsValidator } from '../../../../src/command/rx/001-crosspoint-interrogate-message/params.validator';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import { LocaleData } from '../../../../src/common/locale-data/locale-data.model';

describe('CrossPoint Interrogate Message Command - CommandParamsValidator', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the validator', () => {
            // Arrange
            const params: CrossPointInterrogateMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0
            };
            // Act
            const validator = new CommandParamsValidator(params);

            // Assert
            expect(validator).toBeDefined();
            expect(validator.data).toBe(params);
        });
    });

    describe('validate', () => {
        it('Should succeed with valid params', () => {
            // Arrange
            const params: CrossPointInterrogateMessageCommandParams = {
                matrixId: 1,
                levelId: 1,
                destinationId: 1
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors).toBeDefined();
            expect(Object.keys(errors).length).toBe(0);
        });

        it('Should return errors id params are out of range < MIN', () => {
            // Arrange
            const params: CrossPointInterrogateMessageCommandParams = {
                matrixId: -1,
                levelId: -1,
                destinationId: -1
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.matrixId).toBeDefined();
            expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.levelId).toBeDefined();
            expect(errors.levelId.id).toBe(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.destinationId).toBeDefined();
            expect(errors.destinationId.id).toBe(CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG);
        });

        it('Should return errors if params are out of range > MAX', () => {
            // Arrange
            const params: CrossPointInterrogateMessageCommandParams = {
                matrixId: 256,
                levelId: 256,
                destinationId: 65536
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.matrixId).toBeDefined();
            expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.levelId).toBeDefined();
            expect(errors.levelId.id).toBe(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.destinationId).toBeDefined();
            expect(errors.destinationId.id).toBe(CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG);
        });
    });
});
