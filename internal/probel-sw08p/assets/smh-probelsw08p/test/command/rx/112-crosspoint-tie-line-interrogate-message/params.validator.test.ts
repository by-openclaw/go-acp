import { BootstrapService } from '../../../../src/bootstrap.service';
import { CrossPointTieLineInterrogateMessageCommandParams } from '../../../../src/command/rx/112-crosspoint-tie-line-interrogate-message/params';
import { LocaleData } from '../../../../src/common/locale-data/locale-data.model';
import { CommandParamsValidator } from '../../../../src/command/rx/112-crosspoint-tie-line-interrogate-message/params.validator';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('CrossPoint Tie Line Ineterrogate Message command - CommandParamsValidator', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the validator', () => {
            // Arrange
            const params: CrossPointTieLineInterrogateMessageCommandParams = {
                matrixId: 0,
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
        it('Should succeed with valid params - ...', () => {
            // Arrange
            const params: CrossPointTieLineInterrogateMessageCommandParams = {
                matrixId: 1,
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
            const params: CrossPointTieLineInterrogateMessageCommandParams = {
                matrixId: -1,
                destinationId: -1
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.matrixId).toBeDefined();
            expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.destinationId).toBeDefined();
            expect(errors.destinationId.id).toBe(CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG);
        });

        it('Should return errors if params are out of range > MAX', () => {
            // Arrange
            const params: CrossPointTieLineInterrogateMessageCommandParams = {
                matrixId: 256,
                destinationId: 65536
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.matrixId).toBeDefined();
            expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.destinationId).toBeDefined();
            expect(errors.destinationId.id).toBe(CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG);
        });
    });
});
