import { BootstrapService } from '../../../../src/bootstrap.service';
import { CrossPointGoGroupSalvoMessageCommandParams } from '../../../../src/command/rx/121-crosspoint-go-group-salvo-message/params';
import { LocaleData } from '../../../../src/common/locale-data/locale-data.model';
import { CommandParamsValidator } from '../../../../src/command/rx/121-crosspoint-go-group-salvo-message/params.validator';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('Crosspoint Go Group Salvo Message command - CommandParamsValidator', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the validator', () => {
            // Arrange
            const params: CrossPointGoGroupSalvoMessageCommandParams = {
                salvoId: 0
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
            const params: CrossPointGoGroupSalvoMessageCommandParams = {
                salvoId: 127
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
            const params: CrossPointGoGroupSalvoMessageCommandParams = {
                salvoId: -1
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.salvoId).toBeDefined();
            expect(errors.salvoId.id).toBe(CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG);

        });

        it('Should return errors if params are out of range > MAX', () => {
            // Arrange
            const params: CrossPointGoGroupSalvoMessageCommandParams = {
                salvoId: 256
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.salvoId).toBeDefined();
            expect(errors.salvoId.id).toBe(CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG);
        });
    });
});
