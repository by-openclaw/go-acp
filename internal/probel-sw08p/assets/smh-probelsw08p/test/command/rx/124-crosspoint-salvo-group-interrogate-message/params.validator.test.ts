import { BootstrapService } from '../../../../src/bootstrap.service';
import { CrossPointSalvoGroupInterrogateMessageCommandParams } from '../../../../src/command/rx/124-crosspoint-salvo-group-interrogate-message/params';
import { LocaleData } from '../../../../src/common/locale-data/locale-data.model';
import { CommandParamsValidator } from '../../../../src/command/rx/124-crosspoint-salvo-group-interrogate-message/params.validator';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('CrossPoint Salvo Group Interrogate Message Command - CommandParamsValidator', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the validator', () => {
            // Arrange
            const params: CrossPointSalvoGroupInterrogateMessageCommandParams = {
                salvoId: 0,
                connectIndexId: 0
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
            const params: CrossPointSalvoGroupInterrogateMessageCommandParams = {
                salvoId: 127,
                connectIndexId : 65535
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
            const params: CrossPointSalvoGroupInterrogateMessageCommandParams = {
                salvoId: -1,
                connectIndexId: -1
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.salvoId).toBeDefined();
            expect(errors.salvoId.id).toBe(CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.connectIndexId).toBeDefined();
            expect(errors.connectIndexId.id).toBe(CommandErrorsKeys.CONNECT_INDEX_IS_OUT_OF_RANGE_ERROR_MSG);
        });

        it('Should return errors if params are out of range > MAX', () => {
            // Arrange
            const params: CrossPointSalvoGroupInterrogateMessageCommandParams = {
                salvoId: 128,
                connectIndexId: 65536
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.salvoId).toBeDefined();
            expect(errors.salvoId.id).toBe(CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.connectIndexId).toBeDefined();
            expect(errors.connectIndexId.id).toBe(CommandErrorsKeys.CONNECT_INDEX_IS_OUT_OF_RANGE_ERROR_MSG);
        });
    });
});
